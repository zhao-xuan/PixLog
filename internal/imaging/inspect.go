package imaging

import (
	"encoding/xml"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Manifest struct {
	Schema           string            `json:"schema"`
	ContentOID       string            `json:"content_oid"`
	Size             int64             `json:"size"`
	Format           string            `json:"format"`
	MediaType        string            `json:"media_type"`
	Capability       string            `json:"capability"`
	Width            int               `json:"width,omitempty"`
	Height           int               `json:"height,omitempty"`
	Frames           int               `json:"frames,omitempty"`
	AlphaCapable     bool              `json:"alpha_capable,omitempty"`
	VisualHash       string            `json:"visual_hash,omitempty"`
	EmbeddedMetadata map[string]string `json:"embedded_metadata,omitempty"`
}

var assetExtensions = map[string]string{
	".png": "raster", ".jpg": "raster", ".jpeg": "raster", ".gif": "raster",
	".webp": "raster", ".bmp": "raster", ".tif": "raster", ".tiff": "raster",
	".avif": "raster", ".heic": "raster", ".heif": "raster", ".exr": "raster",
	".svg": "structured", ".svgz": "structured",
	".psd": "structured", ".kra": "structured", ".xcf": "structured", ".ai": "structured",
	".dng": "raw", ".cr2": "raw", ".cr3": "raw", ".nef": "raw", ".arw": "raw", ".raf": "raw",
	".xmp": "sidecar",
}

func IsAsset(path string) bool {
	_, ok := assetExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func InspectFile(path, contentOID string) (Manifest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("inspect %s: %w", path, err)
	}
	extension := strings.ToLower(filepath.Ext(path))
	capability := assetExtensions[extension]
	if capability == "" {
		capability = "opaque"
	}
	format := strings.TrimPrefix(extension, ".")
	if format == "jpg" {
		format = "jpeg"
	}
	mediaType := mime.TypeByExtension(extension)
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	manifest := Manifest{
		Schema:     "pixlog.manifest/v1",
		ContentOID: contentOID,
		Size:       info.Size(),
		Format:     format,
		MediaType:  mediaType,
		Capability: capability,
		Frames:     1,
	}

	if extension == ".svg" {
		manifest.Width, manifest.Height = inspectSVG(path)
		return manifest, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open %s: %w", path, err)
	}
	config, decodedFormat, decodeErr := image.DecodeConfig(file)
	file.Close()
	if decodeErr == nil {
		manifest.Width = config.Width
		manifest.Height = config.Height
		if decodedFormat != "" {
			manifest.Format = decodedFormat
		}
		manifest.AlphaCapable = extension == ".png" || extension == ".gif"
		manifest.VisualHash = perceptualHash(path)
	}
	if extension == ".png" {
		manifest.EmbeddedMetadata = readPNGText(path)
	} else if extension == ".jpg" || extension == ".jpeg" {
		manifest.EmbeddedMetadata = readJPEGMetadata(path)
	} else if extension == ".xmp" {
		manifest.EmbeddedMetadata = readXMPMetadata(path)
	}
	return manifest, nil
}

func inspectSVG(path string) (int, int) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	decoder := xml.NewDecoder(io.LimitReader(file, 1<<20))
	for {
		token, err := decoder.Token()
		if err != nil {
			return 0, 0
		}
		start, ok := token.(xml.StartElement)
		if !ok || strings.ToLower(start.Name.Local) != "svg" {
			continue
		}
		var width, height int
		var viewBox string
		for _, attribute := range start.Attr {
			switch strings.ToLower(attribute.Name.Local) {
			case "width":
				width = parseDimension(attribute.Value)
			case "height":
				height = parseDimension(attribute.Value)
			case "viewbox":
				viewBox = attribute.Value
			}
		}
		if (width == 0 || height == 0) && viewBox != "" {
			parts := strings.Fields(strings.ReplaceAll(viewBox, ",", " "))
			if len(parts) == 4 {
				if value, parseErr := strconv.ParseFloat(parts[2], 64); parseErr == nil {
					width = int(value)
				}
				if value, parseErr := strconv.ParseFloat(parts[3], 64); parseErr == nil {
					height = int(value)
				}
			}
		}
		return width, height
	}
}

func parseDimension(value string) int {
	value = strings.TrimSpace(value)
	end := 0
	for end < len(value) && ((value[end] >= '0' && value[end] <= '9') || value[end] == '.') {
		end++
	}
	if end == 0 {
		return 0
	}
	number, err := strconv.ParseFloat(value[:end], 64)
	if err != nil {
		return 0
	}
	return int(number)
}

func perceptualHash(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	decoded, _, err := image.Decode(file)
	if err != nil {
		return ""
	}
	bounds := decoded.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return ""
	}
	values := make([]uint16, 9*8)
	for y := range 8 {
		for x := range 9 {
			sourceX := bounds.Min.X + x*(bounds.Dx()-1)/8
			sourceY := bounds.Min.Y + y*(bounds.Dy()-1)/7
			red, green, blue, _ := decoded.At(sourceX, sourceY).RGBA()
			values[y*9+x] = uint16((299*uint64(red) + 587*uint64(green) + 114*uint64(blue)) / 1000)
		}
	}
	var hash uint64
	for y := range 8 {
		for x := range 8 {
			hash <<= 1
			if values[y*9+x] > values[y*9+x+1] {
				hash |= 1
			}
		}
	}
	return fmt.Sprintf("dhash:%016x", hash)
}
