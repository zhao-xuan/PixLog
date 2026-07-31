package imaging

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxMetadataPayload = 16 << 20

func readPNGText(path string) map[string]string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	signature := make([]byte, 8)
	if _, err := io.ReadFull(reader, signature); err != nil || string(signature) != "\x89PNG\r\n\x1a\n" {
		return nil
	}
	metadata := map[string]string{}
	for {
		header := make([]byte, 8)
		if _, err := io.ReadFull(reader, header); err != nil {
			break
		}
		length := binary.BigEndian.Uint32(header[:4])
		chunkType := string(header[4:])
		if length > maxMetadataPayload {
			break
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(reader, data); err != nil {
			break
		}
		if _, err := io.CopyN(io.Discard, reader, 4); err != nil {
			break
		}
		switch chunkType {
		case "tEXt":
			if key, value, ok := strings.Cut(string(data), "\x00"); ok && key != "" {
				metadata[key] = value
			}
		case "zTXt":
			if key, value, ok := parseZTxt(data); ok {
				metadata[key] = value
			}
		case "iTXt":
			if key, value, ok := parseITXt(data); ok {
				metadata[key] = value
			}
		case "eXIf":
			mergeMetadata(metadata, parseTIFFMetadata(data))
		case "iCCP":
			if profile, ok := parseICCP(data); ok {
				metadata["ICC.ProfileSHA256"] = metadataDigest(profile)
				metadata["ICC.ProfileSize"] = strconv.Itoa(len(profile))
			}
		case "caBX":
			metadata["C2PA.Present"] = "true"
			metadata["C2PA.StoreSHA256"] = metadataDigest(data)
			metadata["C2PA.StoreSize"] = strconv.Itoa(len(data))
		case "IEND":
			return nonEmptyMetadata(metadata)
		}
	}
	return nonEmptyMetadata(metadata)
}

func parseZTxt(data []byte) (string, string, bool) {
	keywordEnd := bytes.IndexByte(data, 0)
	if keywordEnd <= 0 || keywordEnd > 79 || len(data) <= keywordEnd+2 || data[keywordEnd+1] != 0 {
		return "", "", false
	}
	value, err := decompressMetadata(data[keywordEnd+2:])
	if err != nil {
		return "", "", false
	}
	return string(data[:keywordEnd]), string(value), true
}

func parseITXt(data []byte) (string, string, bool) {
	keywordEnd := bytes.IndexByte(data, 0)
	if keywordEnd <= 0 || keywordEnd > 79 || len(data) < keywordEnd+3 {
		return "", "", false
	}
	keyword := string(data[:keywordEnd])
	remainder := data[keywordEnd+1:]
	compressionFlag, compressionMethod := remainder[0], remainder[1]
	if compressionFlag > 1 || compressionMethod != 0 {
		return "", "", false
	}
	remainder = remainder[2:]
	languageEnd := bytes.IndexByte(remainder, 0)
	if languageEnd < 0 {
		return "", "", false
	}
	remainder = remainder[languageEnd+1:]
	translatedKeywordEnd := bytes.IndexByte(remainder, 0)
	if translatedKeywordEnd < 0 {
		return "", "", false
	}
	value := remainder[translatedKeywordEnd+1:]
	if compressionFlag == 1 {
		decompressed, err := decompressMetadata(value)
		if err != nil {
			return "", "", false
		}
		value = decompressed
	}
	return keyword, string(value), true
}

func parseICCP(data []byte) ([]byte, bool) {
	nameEnd := bytes.IndexByte(data, 0)
	if nameEnd <= 0 || len(data) <= nameEnd+2 || data[nameEnd+1] != 0 {
		return nil, false
	}
	profile, err := decompressMetadata(data[nameEnd+2:])
	return profile, err == nil
}

func decompressMetadata(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	value, err := io.ReadAll(io.LimitReader(reader, maxMetadataPayload+1))
	if err != nil {
		return nil, err
	}
	if len(value) > maxMetadataPayload {
		return nil, fmt.Errorf("metadata exceeds %d bytes", maxMetadataPayload)
	}
	return value, nil
}

func readJPEGMetadata(path string) map[string]string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	signature := make([]byte, 2)
	if _, err := io.ReadFull(reader, signature); err != nil || !bytes.Equal(signature, []byte{0xff, 0xd8}) {
		return nil
	}
	metadata := map[string]string{}
	iccSegments := map[int][]byte{}
	iccCount := 0
	var c2pa bytes.Buffer
	for {
		marker, err := nextJPEGMarker(reader)
		if err != nil || marker == 0xda || marker == 0xd9 {
			break
		}
		if marker >= 0xd0 && marker <= 0xd7 || marker == 0x01 {
			continue
		}
		var lengthBytes [2]byte
		if _, err := io.ReadFull(reader, lengthBytes[:]); err != nil {
			break
		}
		length := int(binary.BigEndian.Uint16(lengthBytes[:])) - 2
		if length < 0 || length > 65533 {
			break
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(reader, data); err != nil {
			break
		}
		switch marker {
		case 0xe1:
			switch {
			case bytes.HasPrefix(data, []byte("Exif\x00\x00")):
				mergeMetadata(metadata, parseTIFFMetadata(data[6:]))
			case bytes.HasPrefix(data, []byte("http://ns.adobe.com/xap/1.0/\x00")):
				mergeMetadata(metadata, parseXMPPacket(data[len("http://ns.adobe.com/xap/1.0/\x00"):]))
			case bytes.HasPrefix(data, []byte("http://ns.adobe.com/xmp/extension/\x00")):
				metadata["XMP.ExtendedPresent"] = "true"
			}
		case 0xe2:
			if bytes.HasPrefix(data, []byte("ICC_PROFILE\x00")) && len(data) >= 14 {
				sequence, count := int(data[12]), int(data[13])
				if sequence > 0 && count > 0 {
					iccSegments[sequence] = append([]byte(nil), data[14:]...)
					iccCount = count
				}
			}
		case 0xed:
			mergeMetadata(metadata, parsePhotoshopIPTC(data))
		case 0xeb:
			if bytes.Contains(bytes.ToLower(data), []byte("jumb")) || bytes.Contains(bytes.ToLower(data), []byte("c2pa")) {
				c2pa.Write(data)
			}
		}
	}
	if iccCount > 0 && len(iccSegments) == iccCount {
		var profile bytes.Buffer
		for sequence := 1; sequence <= iccCount; sequence++ {
			profile.Write(iccSegments[sequence])
		}
		metadata["ICC.ProfileSHA256"] = metadataDigest(profile.Bytes())
		metadata["ICC.ProfileSize"] = strconv.Itoa(profile.Len())
	}
	if c2pa.Len() > 0 {
		metadata["C2PA.Present"] = "true"
		metadata["C2PA.StoreSHA256"] = metadataDigest(c2pa.Bytes())
		metadata["C2PA.StoreSize"] = strconv.Itoa(c2pa.Len())
	}
	return nonEmptyMetadata(metadata)
}

func nextJPEGMarker(reader *bufio.Reader) (byte, error) {
	for {
		prefix, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		if prefix != 0xff {
			continue
		}
		for {
			marker, err := reader.ReadByte()
			if err != nil {
				return 0, err
			}
			if marker == 0xff {
				continue
			}
			if marker == 0x00 {
				break
			}
			return marker, nil
		}
	}
}

func parseTIFFMetadata(data []byte) map[string]string {
	metadata := map[string]string{}
	if len(data) < 8 {
		return metadata
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return metadata
	}
	if order.Uint16(data[2:4]) != 42 {
		return metadata
	}
	tags := map[uint16]string{
		0x010f: "EXIF.Make",
		0x0110: "EXIF.Model",
		0x0112: "EXIF.Orientation",
		0x0131: "EXIF.Software",
		0x0132: "EXIF.DateTime",
		0x013b: "EXIF.Artist",
		0x8298: "EXIF.Copyright",
		0x9003: "EXIF.DateTimeOriginal",
		0x9004: "EXIF.DateTimeDigitized",
		0xa420: "EXIF.ImageUniqueID",
		0xa434: "EXIF.LensModel",
	}
	visited := map[uint32]bool{}
	var parseIFD func(uint32, int)
	parseIFD = func(offset uint32, depth int) {
		if depth > 4 || visited[offset] || int(offset)+2 > len(data) {
			return
		}
		visited[offset] = true
		count := int(order.Uint16(data[offset : offset+2]))
		if count > 1024 || int(offset)+2+count*12 > len(data) {
			return
		}
		for index := 0; index < count; index++ {
			entry := data[int(offset)+2+index*12 : int(offset)+2+(index+1)*12]
			tag := order.Uint16(entry[:2])
			fieldType := order.Uint16(entry[2:4])
			valueCount := order.Uint32(entry[4:8])
			if tag == 0x8769 || tag == 0x8825 {
				childOffset := order.Uint32(entry[8:12])
				if tag == 0x8825 {
					metadata["EXIF.GPSPresent"] = "true"
				} else {
					parseIFD(childOffset, depth+1)
				}
				continue
			}
			name, known := tags[tag]
			if !known {
				continue
			}
			if value, ok := tiffValue(data, order, fieldType, valueCount, entry[8:12]); ok {
				metadata[name] = value
			}
		}
	}
	parseIFD(order.Uint32(data[4:8]), 0)
	return metadata
}

func tiffValue(data []byte, order binary.ByteOrder, fieldType uint16, count uint32, inline []byte) (string, bool) {
	typeSizes := map[uint16]uint32{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8}
	size, exists := typeSizes[fieldType]
	if !exists || count == 0 || count > maxMetadataPayload/size {
		return "", false
	}
	total := size * count
	value := inline
	if total > 4 {
		offset := order.Uint32(inline)
		if uint64(offset)+uint64(total) > uint64(len(data)) {
			return "", false
		}
		value = data[offset : offset+total]
	} else {
		value = inline[:total]
	}
	switch fieldType {
	case 2:
		text := strings.TrimSpace(strings.TrimRight(string(value), "\x00"))
		return text, text != "" && utf8.ValidString(text)
	case 3:
		return strconv.FormatUint(uint64(order.Uint16(value)), 10), true
	case 4:
		return strconv.FormatUint(uint64(order.Uint32(value)), 10), true
	default:
		return metadataDigest(value), true
	}
}

func readXMPMetadata(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxMetadataPayload {
		return nil
	}
	return nonEmptyMetadata(parseXMPPacket(data))
}

func parseXMPPacket(data []byte) map[string]string {
	metadata := map[string]string{
		"XMP.PacketSHA256": metadataDigest(data),
		"XMP.PacketSize":   strconv.Itoa(len(data)),
	}
	wanted := map[string]string{
		"title": "XMP.Title", "description": "XMP.Description", "creator": "XMP.Creator",
		"rights": "XMP.Rights", "creatortool": "XMP.CreatorTool", "rating": "XMP.Rating",
		"label": "XMP.Label", "createdate": "XMP.CreateDate", "modifydate": "XMP.ModifyDate",
		"metadatadate": "XMP.MetadataDate", "format": "XMP.Format", "documentid": "XMP.DocumentID",
		"instanceid": "XMP.InstanceID",
	}
	decoder := xml.NewDecoder(io.LimitReader(bytes.NewReader(data), maxMetadataPayload))
	stack := []string{}
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch typed := token.(type) {
		case xml.StartElement:
			stack = append(stack, strings.ToLower(typed.Name.Local))
			for _, attribute := range typed.Attr {
				if name, ok := wanted[strings.ToLower(attribute.Name.Local)]; ok && strings.TrimSpace(attribute.Value) != "" {
					metadata[name] = strings.TrimSpace(attribute.Value)
				}
			}
		case xml.CharData:
			if len(stack) > 0 {
				if name, ok := wanted[stack[len(stack)-1]]; ok {
					value := strings.TrimSpace(string(typed))
					if value != "" {
						metadata[name] = value
					}
				}
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return metadata
}

func parsePhotoshopIPTC(data []byte) map[string]string {
	metadata := map[string]string{}
	if !bytes.HasPrefix(data, []byte("Photoshop 3.0\x00")) {
		return metadata
	}
	data = data[len("Photoshop 3.0\x00"):]
	for len(data) >= 12 && bytes.Equal(data[:4], []byte("8BIM")) {
		resourceID := binary.BigEndian.Uint16(data[4:6])
		nameLength := int(data[6])
		nameField := 1 + nameLength
		if nameField%2 != 0 {
			nameField++
		}
		sizeOffset := 6 + nameField
		if sizeOffset+4 > len(data) {
			break
		}
		size := int(binary.BigEndian.Uint32(data[sizeOffset : sizeOffset+4]))
		payloadOffset := sizeOffset + 4
		if size < 0 || payloadOffset+size > len(data) {
			break
		}
		if resourceID == 0x0404 {
			mergeMetadata(metadata, parseIPTCDatasets(data[payloadOffset:payloadOffset+size]))
		}
		next := payloadOffset + size
		if next%2 != 0 {
			next++
		}
		if next > len(data) {
			break
		}
		data = data[next:]
	}
	return metadata
}

func parseIPTCDatasets(data []byte) map[string]string {
	metadata := map[string]string{}
	tags := map[byte]string{
		5: "IPTC.ObjectName", 25: "IPTC.Keywords", 55: "IPTC.DateCreated", 80: "IPTC.Byline",
		90: "IPTC.City", 95: "IPTC.State", 101: "IPTC.Country", 105: "IPTC.Headline",
		116: "IPTC.Copyright", 120: "IPTC.Caption",
	}
	for len(data) >= 5 {
		if data[0] != 0x1c {
			data = data[1:]
			continue
		}
		record, dataset := data[1], data[2]
		length := int(binary.BigEndian.Uint16(data[3:5]))
		if length&0x8000 != 0 || 5+length > len(data) {
			break
		}
		if record == 2 {
			if name, ok := tags[dataset]; ok {
				value := strings.TrimSpace(string(data[5 : 5+length]))
				if utf8.ValidString(value) && value != "" {
					if existing := metadata[name]; existing != "" {
						metadata[name] = existing + ", " + value
					} else {
						metadata[name] = value
					}
				}
			}
		}
		data = data[5+length:]
	}
	return metadata
}

func metadataDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", digest)
}

func mergeMetadata(target, source map[string]string) {
	for key, value := range source {
		target[key] = value
	}
}

func nonEmptyMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func sortedMetadataKeys(metadata map[string]string) []string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
