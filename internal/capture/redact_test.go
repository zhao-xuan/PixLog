package capture

import (
	"bytes"
	"io"
	"mime/multipart"
	"testing"
)

func TestRedactPayloadPreservesMultipartBoundaryAndParts(t *testing.T) {
	const boundary = "pixlog-test-boundary"
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	if err := writer.SetBoundary(boundary); err != nil {
		t.Fatalf("SetBoundary: %v", err)
	}
	prompt, _ := writer.CreateFormField("prompt")
	_, _ = io.WriteString(prompt, "studio")
	secret, _ := writer.CreateFormField("api_key")
	_, _ = io.WriteString(secret, "private-token")
	file, _ := writer.CreateFormFile("image", "input.png")
	_, _ = file.Write([]byte("image-bytes"))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	redacted, err := RedactPayload("multipart/form-data; boundary="+boundary, input.Bytes())
	if err != nil {
		t.Fatalf("RedactPayload: %v", err)
	}
	reader := multipart.NewReader(bytes.NewReader(redacted), boundary)
	values := map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		data, _ := io.ReadAll(part)
		values[part.FormName()] = string(data)
	}
	if values["prompt"] != "studio" || values["api_key"] != redactedValue || values["image"] != "image-bytes" {
		t.Fatalf("multipart values = %#v", values)
	}
}
