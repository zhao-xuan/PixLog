package repository

import (
	"strings"
	"testing"
)

func TestPixLogPointerRoundTrip(t *testing.T) {
	pointer := PixLogPointer{
		OID:         "sha256:" + strings.Repeat("a", 64),
		Size:        4821930,
		ManifestOID: "sha256:" + strings.Repeat("b", 64),
		RecipeOID:   "sha256:" + strings.Repeat("c", 64),
		MediaType:   "image/png",
		VisualHash:  "phash:ab4982",
	}
	data, err := pointer.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, found, err := ParsePixLogPointer(data)
	if err != nil {
		t.Fatalf("ParsePixLogPointer: %v", err)
	}
	if !found {
		t.Fatal("pointer was not detected")
	}
	if decoded != pointer {
		t.Fatalf("decoded = %#v, want %#v", decoded, pointer)
	}
}

func TestParsePixLogPointerDistinguishesImageData(t *testing.T) {
	if _, found, err := ParsePixLogPointer([]byte("not a pointer")); err != nil || found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
}

func TestPixLogPointerRejectsInvalidRequiredFields(t *testing.T) {
	data := []byte("version " + PixLogPointerVersion + "\noid sha256:bad\nsize 1\npixlog.manifest sha256:" + strings.Repeat("b", 64) + "\n")
	if _, found, err := ParsePixLogPointer(data); !found || err == nil {
		t.Fatalf("found = %v, err = %v", found, err)
	}
}
