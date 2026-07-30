package imaging

import (
	"bytes"
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

func pngChunk(chunkType string, data []byte) []byte {
	chunk := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(data)))
	copy(chunk[4:8], chunkType)
	copy(chunk[8:], data)
	checksumInput := append([]byte(chunkType), data...)
	binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(checksumInput))
	return chunk
}
