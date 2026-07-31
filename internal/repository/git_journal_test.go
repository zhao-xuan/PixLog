package repository

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/pixlog/pixlog/internal/recipe"
)

func TestProvenanceJournalRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	journal, err := OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	defer journal.Close()
	contentOID := "sha256:" + strings.Repeat("a", 64)
	recipeOID := "sha256:" + strings.Repeat("b", 64)
	if err := journal.Record(contentOID, recipeOID, "assets/hero.png"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	loaded, found, err := journal.RecipeForContent(contentOID)
	if err != nil {
		t.Fatalf("RecipeForContent: %v", err)
	}
	if !found || loaded != recipeOID {
		t.Fatalf("loaded = %q, found = %v", loaded, found)
	}
}

func TestCaptureJournalStoresOrderedEventsAndArtifacts(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	journal, err := OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	defer journal.Close()
	session, err := journal.StartCaptureSession(CaptureSession{
		Adapter:        "photoshop-uxp",
		AdapterVersion: "0.1.0",
		Application:    "Adobe Photoshop",
		DocumentID:     "84",
		Metadata:       json.RawMessage(`{"width":2048,"height":2048}`),
	})
	if err != nil {
		t.Fatalf("StartCaptureSession: %v", err)
	}
	for _, eventType := range []string{"open", "curves"} {
		if _, err := journal.AppendCaptureEvent(CaptureEvent{
			SessionID:  session.ID,
			EventType:  eventType,
			Fidelity:   recipe.FidelityExactCommand,
			Normalized: json.RawMessage(`{"operation":"color.curves"}`),
		}); err != nil {
			t.Fatalf("AppendCaptureEvent(%s): %v", eventType, err)
		}
	}
	events, err := journal.CaptureEvents(session.ID)
	if err != nil {
		t.Fatalf("CaptureEvents: %v", err)
	}
	if len(events) != 2 || events[0].Sequence != 1 || events[1].Sequence != 2 {
		t.Fatalf("events = %#v", events)
	}
	contentOID := "sha256:" + strings.Repeat("c", 64)
	artifact, err := journal.RecordCaptureArtifact(CaptureArtifact{
		SessionID:  session.ID,
		EventID:    events[1].ID,
		Role:       "output",
		ContentOID: contentOID,
		AssetPath:  "assets/hero.png",
	})
	if err != nil || artifact.ID == "" {
		t.Fatalf("RecordCaptureArtifact = %#v, %v", artifact, err)
	}
}
