package capture

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/pixlog/pixlog/internal/repository"
)

func TestProxyForwardsOriginalAndCapturesRedactedPayloads(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	var upstreamBody []byte
	var upstreamAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamAuthorization = request.Header.Get("Authorization")
		upstreamBody, _ = io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"prompt_id":"prompt-123","status":"queued","output_url":"https://cdn.example.test/result.png?token=response-secret"}`)
	}))
	defer upstream.Close()

	root := t.TempDir()
	runCaptureGit(t, root, "init", "--quiet")
	proxy, err := NewProxy(root, ProxyOptions{Platform: "comfyui", Upstream: upstream.URL})
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	proxyServer := httptest.NewServer(proxy.Handler())
	defer proxyServer.Close()
	defer proxy.Close()

	originalBody := `{"prompt":{"text":"studio"},"api_key":"request-secret"}`
	request, err := http.NewRequest(http.MethodPost, proxyServer.URL+"/prompt?token=query-secret", strings.NewReader(originalBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer provider-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	response.Body.Close()
	if upstreamAuthorization != "Bearer provider-secret" || string(upstreamBody) != originalBody {
		t.Fatalf("upstream auth/body = %q, %s", upstreamAuthorization, upstreamBody)
	}

	journal, err := repository.OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	defer journal.Close()
	events, err := journal.CaptureEvents(proxy.SessionID())
	if err != nil {
		t.Fatalf("CaptureEvents: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "comfyui.prompt" {
		t.Fatalf("events = %#v", events)
	}
	store, err := repository.OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	requestPayload, err := store.Get(events[0].RawPayloadOID)
	if err != nil {
		t.Fatalf("Get request payload: %v", err)
	}
	if bytes.Contains(requestPayload, []byte("request-secret")) || !bytes.Contains(requestPayload, []byte(redactedValue)) {
		t.Fatalf("captured request = %s", requestPayload)
	}
	var normalized map[string]any
	if err := json.Unmarshal(events[0].Normalized, &normalized); err != nil {
		t.Fatalf("decode normalized event: %v", err)
	}
	if strings.Contains(normalized["path"].(string), "query-secret") {
		t.Fatalf("captured path = %q", normalized["path"])
	}
	responsePayload, err := store.Get(normalized["response_oid"].(string))
	if err != nil {
		t.Fatalf("Get response payload: %v", err)
	}
	if bytes.Contains(responsePayload, []byte("response-secret")) {
		t.Fatalf("captured response = %s", responsePayload)
	}
}

func TestProxyRecordsA1111Base64ImageAsOutputArtifact(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	var imageBuffer bytes.Buffer
	output := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	output.SetNRGBA(0, 0, color.NRGBA{R: 18, G: 52, B: 86, A: 255})
	if err := png.Encode(&imageBuffer, output); err != nil {
		t.Fatalf("encode output: %v", err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"images": []string{base64.StdEncoding.EncodeToString(imageBuffer.Bytes())},
		})
	}))
	defer upstream.Close()

	root := t.TempDir()
	runCaptureGit(t, root, "init", "--quiet")
	proxy, err := NewProxy(root, ProxyOptions{Platform: "automatic1111", Upstream: upstream.URL})
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer proxy.Close()
	proxyServer := httptest.NewServer(proxy.Handler())
	defer proxyServer.Close()
	response, err := http.Post(proxyServer.URL+"/sdapi/v1/txt2img", "application/json", strings.NewReader(`{"prompt":"studio"}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()

	journal, err := repository.OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	defer journal.Close()
	artifacts, err := journal.CaptureArtifacts(proxy.SessionID())
	if err != nil {
		t.Fatalf("CaptureArtifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Role != "output" {
		t.Fatalf("artifacts = %#v", artifacts)
	}
	store, err := repository.OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	stored, err := store.Get(artifacts[0].ContentOID)
	if err != nil {
		t.Fatalf("Get artifact: %v", err)
	}
	if !bytes.Equal(stored, imageBuffer.Bytes()) {
		t.Fatal("stored output artifact differs from provider image")
	}
}
