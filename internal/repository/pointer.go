package repository

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	PixLogPointerVersion = "https://git-lfs.github.com/spec/v1"
	MaxPointerSize       = 1024
)

type PixLogPointer struct {
	OID         string
	Size        int64
	ManifestOID string
	RecipeOID   string
	MediaType   string
	VisualHash  string
}

func (pointer PixLogPointer) Encode() ([]byte, error) {
	if err := pointer.Validate(); err != nil {
		return nil, err
	}
	var output strings.Builder
	fmt.Fprintf(&output, "version %s\n", PixLogPointerVersion)
	fmt.Fprintf(&output, "oid %s\n", pointer.OID)
	fmt.Fprintf(&output, "size %d\n", pointer.Size)
	fmt.Fprintf(&output, "pixlog.manifest %s\n", pointer.ManifestOID)
	if pointer.MediaType != "" {
		fmt.Fprintf(&output, "pixlog.media %s\n", pointer.MediaType)
	}
	if pointer.RecipeOID != "" {
		fmt.Fprintf(&output, "pixlog.recipe %s\n", pointer.RecipeOID)
	}
	if pointer.VisualHash != "" {
		fmt.Fprintf(&output, "pixlog.visual %s\n", pointer.VisualHash)
	}
	data := []byte(output.String())
	if len(data) > MaxPointerSize {
		return nil, fmt.Errorf("PixLog pointer exceeds %d bytes", MaxPointerSize)
	}
	return data, nil
}

func (pointer PixLogPointer) Validate() error {
	if _, err := parseOID(pointer.OID); err != nil {
		return fmt.Errorf("invalid pointer blob OID: %w", err)
	}
	if _, err := parseOID(pointer.ManifestOID); err != nil {
		return fmt.Errorf("invalid pointer manifest OID: %w", err)
	}
	if pointer.RecipeOID != "" {
		if _, err := parseOID(pointer.RecipeOID); err != nil {
			return fmt.Errorf("invalid pointer recipe OID: %w", err)
		}
	}
	if pointer.Size < 0 {
		return errors.New("pointer size cannot be negative")
	}
	if strings.ContainsAny(pointer.MediaType, "\r\n") || strings.ContainsAny(pointer.VisualHash, "\r\n") {
		return errors.New("pointer values cannot contain newlines")
	}
	return nil
}

func ParsePixLogPointer(data []byte) (PixLogPointer, bool, error) {
	if len(data) > MaxPointerSize {
		return PixLogPointer{}, false, nil
	}
	if !bytes.HasPrefix(data, []byte("version "+PixLogPointerVersion+"\n")) {
		return PixLogPointer{}, false, nil
	}

	values := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		key, value, found := strings.Cut(line, " ")
		if !found || key == "" || value == "" {
			return PixLogPointer{}, true, fmt.Errorf("invalid PixLog pointer line %q", line)
		}
		if _, duplicate := values[key]; duplicate {
			return PixLogPointer{}, true, fmt.Errorf("duplicate PixLog pointer field %q", key)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return PixLogPointer{}, true, err
	}
	if values["version"] != PixLogPointerVersion {
		return PixLogPointer{}, true, errors.New("unsupported PixLog pointer version")
	}
	size, err := strconv.ParseInt(values["size"], 10, 64)
	if err != nil {
		return PixLogPointer{}, true, fmt.Errorf("invalid PixLog pointer size: %w", err)
	}
	pointer := PixLogPointer{
		OID:         values["oid"],
		Size:        size,
		ManifestOID: values["pixlog.manifest"],
		RecipeOID:   values["pixlog.recipe"],
		MediaType:   values["pixlog.media"],
		VisualHash:  values["pixlog.visual"],
	}
	if err := pointer.Validate(); err != nil {
		return PixLogPointer{}, true, err
	}
	return pointer, true, nil
}
