package imaging

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestReadPNGTextParsesUncompressedITXt(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	base.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, base); err != nil {
		t.Fatalf("encode base PNG: %v", err)
	}
	workflow := `{"nodes":[]}`
	chunkData := append([]byte("workflow\x00"), 0, 0)
	chunkData = append(chunkData, 0)
	chunkData = append(chunkData, 0)
	chunkData = append(chunkData, workflow...)
	chunk := pngChunk("iTXt", chunkData)
	baseBytes := encoded.Bytes()
	withMetadata := append([]byte{}, baseBytes[:len(baseBytes)-12]...)
	withMetadata = append(withMetadata, chunk...)
	withMetadata = append(withMetadata, baseBytes[len(baseBytes)-12:]...)

	filePath := filepath.Join(t.TempDir(), "workflow.png")
	if err := os.WriteFile(filePath, withMetadata, 0o644); err != nil {
		t.Fatalf("write PNG: %v", err)
	}
	metadata := readPNGText(filePath)
	if metadata["workflow"] != workflow {
		t.Fatalf("workflow metadata = %q", metadata["workflow"])
	}
	if _, _, err := image.Decode(bytes.NewReader(withMetadata)); err != nil {
		t.Fatalf("fixture is not a valid PNG: %v", err)
	}
}

func TestReadPNGTextParsesCompressedTextAndC2PA(t *testing.T) {
	base := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, base); err != nil {
		t.Fatalf("encode base PNG: %v", err)
	}
	var compressed bytes.Buffer
	compressor := zlib.NewWriter(&compressed)
	_, _ = compressor.Write([]byte("prompt data"))
	if err := compressor.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}
	zText := append([]byte("parameters\x00\x00"), compressed.Bytes()...)
	baseBytes := encoded.Bytes()
	withMetadata := append([]byte{}, baseBytes[:len(baseBytes)-12]...)
	withMetadata = append(withMetadata, pngChunk("zTXt", zText)...)
	withMetadata = append(withMetadata, pngChunk("caBX", []byte("jumb-c2pa-store"))...)
	withMetadata = append(withMetadata, baseBytes[len(baseBytes)-12:]...)

	filePath := filepath.Join(t.TempDir(), "metadata.png")
	if err := os.WriteFile(filePath, withMetadata, 0o644); err != nil {
		t.Fatalf("write PNG: %v", err)
	}
	metadata := readPNGText(filePath)
	if metadata["parameters"] != "prompt data" || metadata["C2PA.Present"] != "true" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestReadJPEGMetadataParsesEXIFAndXMP(t *testing.T) {
	tiff := make([]byte, 64)
	copy(tiff[:2], "II")
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 3)
	writeTIFFASCIIEntry(tiff[10:22], 0x010f, 50, []byte("PixLog\x00"))
	writeTIFFASCIIEntry(tiff[22:34], 0x0110, 57, []byte("Camera\x00"))
	binary.LittleEndian.PutUint16(tiff[34:36], 0x0112)
	binary.LittleEndian.PutUint16(tiff[36:38], 3)
	binary.LittleEndian.PutUint32(tiff[38:42], 1)
	binary.LittleEndian.PutUint16(tiff[42:44], 6)
	copy(tiff[50:57], []byte("PixLog\x00"))
	copy(tiff[57:64], []byte("Camera\x00"))

	xmp := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Hero</dc:title></x:xmpmeta>`)
	jpeg := []byte{0xff, 0xd8}
	jpeg = append(jpeg, jpegSegment(0xe1, append([]byte("Exif\x00\x00"), tiff...))...)
	jpeg = append(jpeg, jpegSegment(0xe1, append([]byte("http://ns.adobe.com/xap/1.0/\x00"), xmp...))...)
	jpeg = append(jpeg, 0xff, 0xd9)
	filePath := filepath.Join(t.TempDir(), "metadata.jpg")
	if err := os.WriteFile(filePath, jpeg, 0o644); err != nil {
		t.Fatalf("write JPEG: %v", err)
	}
	metadata := readJPEGMetadata(filePath)
	if metadata["EXIF.Make"] != "PixLog" || metadata["EXIF.Model"] != "Camera" || metadata["EXIF.Orientation"] != "6" || metadata["XMP.Title"] != "Hero" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func writeTIFFASCIIEntry(entry []byte, tag uint16, offset uint32, value []byte) {
	binary.LittleEndian.PutUint16(entry[:2], tag)
	binary.LittleEndian.PutUint16(entry[2:4], 2)
	binary.LittleEndian.PutUint32(entry[4:8], uint32(len(value)))
	binary.LittleEndian.PutUint32(entry[8:12], offset)
}

func jpegSegment(marker byte, data []byte) []byte {
	segment := []byte{0xff, marker, 0, 0}
	binary.BigEndian.PutUint16(segment[2:4], uint16(len(data)+2))
	return append(segment, data...)
}

func pngChunk(chunkType string, data []byte) []byte {
	chunk := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(data)))
	copy(chunk[4:8], chunkType)
	copy(chunk[8:], data)
	checksumInput := append([]byte(chunkType), data...)
	binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(checksumInput))
	return chunk
}
