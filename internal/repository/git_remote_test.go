package repository

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitMediaRemotePushAndFetch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	remoteRoot := filepath.Join(t.TempDir(), "artwork.git")
	runGitTest(t, root, "init", "--quiet")
	if err := os.MkdirAll(remoteRoot, 0o755); err != nil {
		t.Fatalf("create bare remote directory: %v", err)
	}
	runGitTest(t, remoteRoot, "init", "--quiet", "--bare")
	runGitTest(t, root, "config", "user.name", "PixLog Test")
	runGitTest(t, root, "config", "user.email", "pixlog@example.test")
	runGitTest(t, root, "remote", "add", "origin", remoteRoot)
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	filterCommand := shellQuote(testExecutable) + " -test.run=^TestGitFilterProcessHelper$ --"
	runGitTest(t, root, "config", "filter.pixlog.process", filterCommand)
	runGitTest(t, root, "config", "filter.pixlog.required", "true")
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.png filter=pixlog -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}
	imageData := encodeMediaTestPNG(t)
	store, err := OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	parentOID, err := store.Put([]byte("reference image bytes"))
	if err != nil {
		t.Fatalf("store parent: %v", err)
	}
	rawPayloadOID, err := store.Put([]byte(`{"request":"captured"}`))
	if err != nil {
		t.Fatalf("store raw payload: %v", err)
	}
	recipeData, err := json.Marshal(map[string]any{
		"schema": "pixlog.recipe/v1",
		"kind":   "ai-generation",
		"parents": []any{map[string]any{
			"asset": parentOID,
			"role":  "reference-image",
		}},
		"vendor": map[string]any{"raw_payload_oid": rawPayloadOID},
	})
	if err != nil {
		t.Fatalf("marshal recipe: %v", err)
	}
	recipeOID, err := store.Put(recipeData)
	if err != nil {
		t.Fatalf("store recipe: %v", err)
	}
	journal, err := OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	if err := journal.Record(hashBytes(imageData), recipeOID, "hero.png"); err != nil {
		journal.Close()
		t.Fatalf("record recipe: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("close provenance journal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "hero.png"), imageData, 0o644); err != nil {
		t.Fatalf("write hero.png: %v", err)
	}
	runGitFilterTestCommand(t, root, "add", ".gitattributes", "hero.png")
	runGitFilterTestCommand(t, root, "commit", "--quiet", "-m", "add image")
	pointerData, err := gitBytes(root, "show", "HEAD:hero.png")
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	pointer, found, err := ParsePixLogPointer(pointerData)
	if err != nil || !found {
		t.Fatalf("pointer found = %v, err = %v", found, err)
	}
	head := strings.TrimSpace(runGitTestOutput(t, root, "rev-parse", "HEAD"))
	branch := strings.TrimSpace(runGitTestOutput(t, root, "symbolic-ref", "--short", "HEAD"))
	repo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	result, err := repo.PushMediaObjects("origin", []GitRefUpdate{{LocalRef: "refs/heads/" + branch, LocalOID: head, RemoteRef: "refs/heads/" + branch, RemoteOID: strings.Repeat("0", 40)}})
	if err != nil {
		t.Fatalf("PushMediaObjects: %v", err)
	}
	if len(result.Objects) < 2 || result.Bytes == 0 {
		t.Fatalf("transfer result = %#v", result)
	}
	localObjectPath, err := store.ObjectPath(pointer.OID)
	if err != nil {
		t.Fatalf("ObjectPath: %v", err)
	}
	if _, err := repo.Dehydrate([]string{"hero.png"}); err != nil {
		t.Fatalf("Dehydrate: %v", err)
	}
	dehydrated, err := os.ReadFile(filepath.Join(root, "hero.png"))
	if err != nil {
		t.Fatalf("read dehydrated worktree file: %v", err)
	}
	if _, found, err := ParsePixLogPointer(dehydrated); err != nil || !found {
		t.Fatalf("dehydrated pointer found = %v, err = %v", found, err)
	}
	if err := os.Remove(localObjectPath); err != nil {
		t.Fatalf("remove local object: %v", err)
	}
	if _, err := repo.Hydrate([]string{"hero.png"}); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(root, "hero.png"))
	if err != nil {
		t.Fatalf("read hydrated worktree file: %v", err)
	}
	if !bytes.Equal(restored, imageData) {
		t.Fatal("remote object did not restore exact image bytes")
	}
	for _, oid := range []string{parentOID, rawPayloadOID} {
		digest := strings.TrimPrefix(oid, "sha256:")
		remoteObjectPath := filepath.Join(remoteRoot+".pixlog", "objects", "sha256", digest[:2], digest[2:])
		if _, err := os.Stat(remoteObjectPath); err != nil {
			t.Fatalf("nested recipe object %s was not uploaded: %v", oid, err)
		}
	}
}

func TestGitMediaHTTPBatchRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	store, err := OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	objectData := []byte("PixLog HTTP batch object")
	oid, err := store.Put(objectData)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	digest := strings.TrimPrefix(oid, "sha256:")
	remoteObjects := map[string][]byte{}
	verified := false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/objects/batch" && request.Method == http.MethodPost:
			var payload batchRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Errorf("decode batch request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			object := payload.Objects[0]
			object.Actions = map[string]batchAction{}
			if payload.Operation == "upload" {
				object.Actions["upload"] = batchAction{Href: server.URL + "/upload/" + object.OID, Header: map[string]string{"X-PixLog-Test": "upload"}}
				object.Actions["verify"] = batchAction{Href: server.URL + "/verify/" + object.OID}
			} else {
				object.Actions["download"] = batchAction{Href: server.URL + "/download/" + object.OID, Header: map[string]string{"X-PixLog-Test": "download"}}
			}
			writer.Header().Set("Content-Type", "application/vnd.git-lfs+json")
			json.NewEncoder(writer).Encode(batchResponse{Objects: []batchObject{object}})
		case strings.HasPrefix(request.URL.Path, "/upload/") && request.Method == http.MethodPut:
			if request.Header.Get("X-PixLog-Test") != "upload" {
				t.Error("upload action header was not forwarded")
			}
			data, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read upload: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			remoteObjects[strings.TrimPrefix(request.URL.Path, "/upload/")] = data
			writer.WriteHeader(http.StatusOK)
		case strings.HasPrefix(request.URL.Path, "/verify/") && request.Method == http.MethodPost:
			verified = true
			writer.WriteHeader(http.StatusOK)
		case strings.HasPrefix(request.URL.Path, "/download/") && request.Method == http.MethodGet:
			if request.Header.Get("X-PixLog-Test") != "download" {
				t.Error("download action header was not forwarded")
			}
			data, exists := remoteObjects[strings.TrimPrefix(request.URL.Path, "/download/")]
			if !exists {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			writer.Write(data)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	transferred, err := uploadHTTPBatch(server.URL, store, []string{oid})
	if err != nil {
		t.Fatalf("uploadHTTPBatch: %v", err)
	}
	if transferred != int64(len(objectData)) || !verified || !bytes.Equal(remoteObjects[digest], objectData) {
		t.Fatalf("upload bytes = %d, verified = %v, remote = %q", transferred, verified, remoteObjects[digest])
	}
	if err := removeMediaObject(store, oid); err != nil {
		t.Fatalf("removeMediaObject: %v", err)
	}
	if err := downloadHTTPBatch(server.URL, store, oid); err != nil {
		t.Fatalf("downloadHTTPBatch: %v", err)
	}
	restored, err := store.Get(oid)
	if err != nil || !bytes.Equal(restored, objectData) {
		t.Fatalf("restored = %q, err = %v", restored, err)
	}
}
