package cli

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/pixlog/pixlog/internal/recipe"
	"github.com/pixlog/pixlog/internal/repository"
)

func TestExecuteCapturedRequestUsesExplicitEndpointAndEnvironmentAuth(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	var authorization, requestBody string
	imageData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		data, _ := io.ReadAll(request.Body)
		requestBody = string(data)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"job-replayed","status":"queued","data":[{"b64_json":"`+base64.StdEncoding.EncodeToString(imageData)+`"}]}`)
	}))
	defer upstream.Close()

	root := t.TempDir()
	runCLIGit(t, root, "init", "--quiet")
	repo, err := repository.OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	store, err := repository.OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	requestOID, err := store.Put([]byte(`{"prompt":"studio"}`))
	if err != nil {
		t.Fatalf("Put request: %v", err)
	}
	plan := repository.ReproductionPlan{
		RecipeOID: "sha256:" + strings.Repeat("a", 64),
		Request: &repository.ReproductionRequest{
			Provider: "openai", Adapter: "openai-proxy", Method: "POST", Path: "/v1/images/generations",
			ContentType: "application/json", RequestOID: requestOID, Fidelity: recipe.FidelityExactRequest,
		},
	}
	t.Setenv("PIXLOG_TEST_API_TOKEN", "provider-secret")
	var stdout bytes.Buffer
	if err := executeCapturedRequest(repo, plan, upstream.URL, "PIXLOG_TEST_API_TOKEN", "", time.Second, &stdout); err != nil {
		t.Fatalf("executeCapturedRequest: %v", err)
	}
	if authorization != "Bearer provider-secret" || requestBody != `{"prompt":"studio"}` {
		t.Fatalf("authorization/body = %q, %q", authorization, requestBody)
	}
	if !strings.Contains(stdout.String(), "job-replayed") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	journal, err := repository.OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	defer journal.Close()
	sessions, err := journal.CaptureSessions(10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("CaptureSessions = %#v, %v", sessions, err)
	}
	artifacts, err := journal.CaptureArtifacts(sessions[0].ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("CaptureArtifacts = %#v, %v", artifacts, err)
	}
	storedImage, err := store.Get(artifacts[0].ContentOID)
	if err != nil || !bytes.Equal(storedImage, imageData) {
		t.Fatalf("stored replay image differs: %v", err)
	}
}

func TestIncompleteReproductionPayloadRejectsEncodedAndDigestOnlyData(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte("api_key=%5BREDACTED%5D"),
		[]byte(`{"capture":"digest-only-unstructured-payload","sha256":"sha256:abc"}`),
	} {
		if !incompleteReproductionPayload(payload) {
			t.Errorf("payload was considered replayable: %s", payload)
		}
	}
}
