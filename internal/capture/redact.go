package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"regexp"
	"strings"
)

const redactedValue = "[REDACTED]"

var textSecretPattern = regexp.MustCompile(`(?i)(^|[^a-z0-9])(authorization|(?:x[ _-]?)?api[ _-]?key|aws[ _-]?access[ _-]?key[ _-]?id|key|(?:[a-z0-9]+[ _-])?token|client[ _-]?secret|secret|password|passwd|(?:set[ _-]?|session[ _-]?)?cookie|credential|signature|sig|webhook)([ \t]*[:=][ \t]*)([^\r\n]+)`)

var sensitiveKeys = map[string]struct{}{
	"authorization":  {},
	"apikey":         {},
	"key":            {},
	"token":          {},
	"accesstoken":    {},
	"refreshtoken":   {},
	"clientsecret":   {},
	"secret":         {},
	"password":       {},
	"passwd":         {},
	"cookie":         {},
	"setcookie":      {},
	"sessioncookie":  {},
	"sessiontoken":   {},
	"signedtoken":    {},
	"signature":      {},
	"sig":            {},
	"xamzsignature":  {},
	"xgoogsignature": {},
}

func RedactJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode payload for redaction: %w", err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("decode payload for redaction: multiple JSON values")
	}
	value = redactValue(value)
	redacted, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode redacted payload: %w", err)
	}
	return redacted, nil
}

func RedactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.RawQuery == "" {
		return rawURL
	}
	query := parsed.Query()
	changed := false
	for key := range query {
		if isSensitiveKey(key) {
			query.Set(key, redactedValue)
			changed = true
		}
	}
	if !changed {
		return rawURL
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func RedactText(data []byte) []byte {
	return textSecretPattern.ReplaceAll(data, []byte("$1$2$3"+redactedValue))
}

func RedactPayload(contentType string, data []byte) ([]byte, error) {
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil {
		return digestOnlyPayload(contentType, data), nil
	}
	switch mediaType {
	case "application/json", "application/ld+json", "application/problem+json":
		return RedactJSON(data)
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(data))
		if err != nil {
			return nil, fmt.Errorf("decode form payload for redaction: %w", err)
		}
		for key, entries := range values {
			if isSensitiveKey(key) {
				values[key] = []string{redactedValue}
				continue
			}
			for index, value := range entries {
				entries[index] = RedactURL(value)
			}
		}
		return []byte(values.Encode()), nil
	case "multipart/form-data":
		boundary := parameters["boundary"]
		if boundary == "" {
			return nil, errors.New("multipart payload has no boundary")
		}
		return redactMultipart(data, boundary)
	case "application/octet-stream", "image/png", "image/jpeg", "image/webp":
		return append([]byte(nil), data...), nil
	default:
		return digestOnlyPayload(contentType, data), nil
	}
}

func redactMultipart(data []byte, boundary string) ([]byte, error) {
	reader := multipart.NewReader(bytes.NewReader(data), boundary)
	var output bytes.Buffer
	writer := multipart.NewWriter(&output)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, fmt.Errorf("preserve multipart boundary: %w", err)
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode multipart payload for redaction: %w", err)
		}
		target, err := writer.CreatePart(part.Header)
		if err != nil {
			part.Close()
			return nil, fmt.Errorf("encode redacted multipart payload: %w", err)
		}
		if isSensitiveKey(part.FormName()) {
			_, err = io.WriteString(target, redactedValue)
		} else {
			_, err = io.Copy(target, part)
		}
		part.Close()
		if err != nil {
			return nil, fmt.Errorf("encode redacted multipart field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish redacted multipart payload: %w", err)
	}
	return output.Bytes(), nil
}

func digestOnlyPayload(contentType string, data []byte) []byte {
	digest := sha256.Sum256(data)
	document := map[string]any{
		"capture":      "digest-only-unstructured-payload",
		"content_type": contentType,
		"sha256":       fmt.Sprintf("sha256:%x", digest),
		"size":         len(data),
	}
	encoded, _ := json.Marshal(document)
	return encoded
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if isSensitiveKey(key) {
				typed[key] = redactedValue
				continue
			}
			typed[key] = redactValue(child)
		}
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = redactValue(child)
		}
		return typed
	case string:
		return RedactURL(typed)
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, key)
	_, sensitive := sensitiveKeys[normalized]
	return sensitive
}
