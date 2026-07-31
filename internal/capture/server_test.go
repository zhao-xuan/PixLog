package capture

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/pixlog/pixlog/internal/repository"
)

func TestServerCapturesRedactedEventAndRecipe(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCaptureGit(t, root, "init", "--quiet")
	server, err := NewServer(root, Options{Token: "test-token"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	sessionResponse := captureRequest(t, httpServer.URL+"/v1/sessions", "test-token", `{
		"adapter":"photoshop-uxp",
		"adapter_version":"0.1.0",
		"application":"Adobe Photoshop",
		"document_id":"84"
	}`)
	var session repository.CaptureSession
	decodeCaptureResponse(t, sessionResponse, http.StatusCreated, &session)

	eventResponse := captureRequest(t, httpServer.URL+"/v1/events", "test-token", `{
		"session_id":"`+session.ID+`",
		"event_type":"curves",
		"fidelity":"exact-command",
		"raw_payload":{"authorization":"Bearer secret","callback":"https://example.test/output?token=secret"},
		"normalized":{"operation":"color.curves"}
	}`)
	var event repository.CaptureEvent
	decodeCaptureResponse(t, eventResponse, http.StatusCreated, &event)
	if event.RawPayloadOID == "" {
		t.Fatal("event raw payload was not stored")
	}
	store, err := repository.OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	rawPayload, err := store.Get(event.RawPayloadOID)
	if err != nil {
		t.Fatalf("Get raw payload: %v", err)
	}
	if bytes.Contains(rawPayload, []byte("secret")) || !bytes.Contains(rawPayload, []byte(redactedValue)) {
		t.Fatalf("raw payload was not redacted: %s", rawPayload)
	}

	contentOID := "sha256:" + strings.Repeat("a", 64)
	recipeResponse := captureRequest(t, httpServer.URL+"/v1/recipes", "test-token", `{
		"content_oid":"`+contentOID+`",
		"asset_path":"assets/hero.png",
		"recipe":{
			"kind":"image-edit",
			"capture":{"adapter":"photoshop-uxp","fidelity":"exact-command"},
			"reproducibility":{"status":"best-effort"}
		},
		"raw_payload":{"api_key":"do-not-store"}
	}`)
	var recipeResult map[string]string
	decodeCaptureResponse(t, recipeResponse, http.StatusCreated, &recipeResult)
	journal, err := repository.OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	defer journal.Close()
	associated, found, err := journal.RecipeForContent(contentOID)
	if err != nil || !found || associated != recipeResult["recipe_oid"] {
		t.Fatalf("recipe association = %q, %v, %v", associated, found, err)
	}
}

func TestServerRequiresConfiguredToken(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCaptureGit(t, root, "init", "--quiet")
	server, err := NewServer(root, Options{Token: "expected"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()
	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func captureRequest(t *testing.T, endpoint, token, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(pixlogTokenHeader, token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return response
}

func decodeCaptureResponse(t *testing.T, response *http.Response, status int, target any) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != status {
		var failure map[string]any
		_ = json.NewDecoder(response.Body).Decode(&failure)
		t.Fatalf("status = %d, want %d: %#v", response.StatusCode, status, failure)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func runCaptureGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
