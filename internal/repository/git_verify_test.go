package repository

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestGitVerifyAndDoctorDetectCorruptObject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	if _, err := InstallGitIntegration(root, "/usr/local/bin/pixlog"); err != nil {
		t.Fatalf("InstallGitIntegration: %v", err)
	}
	repo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	pointerData, pointer, err := repo.CleanFilter("hero.png", mergeTestPNG(t, map[image.Point]color.NRGBA{{X: 1, Y: 1}: {R: 255, A: 255}}))
	if err != nil {
		t.Fatalf("CleanFilter: %v", err)
	}
	blobOID, err := gitOutputWithInput(root, pointerData, "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatalf("hash pointer: %v", err)
	}
	if _, err := gitOutput(root, "update-index", "--add", "--cacheinfo", "100644,"+blobOID+",hero.png"); err != nil {
		t.Fatalf("update index: %v", err)
	}
	verification, err := repo.VerifyObjects()
	if err != nil {
		t.Fatalf("VerifyObjects: %v", err)
	}
	if verification.Objects < 2 || len(verification.Corrupt) != 0 {
		t.Fatalf("verification = %#v", verification)
	}
	doctor, err := repo.Doctor()
	if err != nil || !doctor.Passed {
		t.Fatalf("Doctor = %#v, err = %v", doctor, err)
	}
	store, err := OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	objectPath, err := store.ObjectPath(pointer.OID)
	if err != nil {
		t.Fatalf("ObjectPath: %v", err)
	}
	if err := os.WriteFile(objectPath, []byte("corrupt"), 0o644); err != nil {
		t.Fatalf("corrupt object: %v", err)
	}
	verification, err = repo.VerifyObjects()
	if err != nil {
		t.Fatalf("VerifyObjects after corruption: %v", err)
	}
	if len(verification.Corrupt) == 0 {
		t.Fatal("corrupt object was not reported")
	}
	doctor, err = repo.Doctor()
	if err != nil {
		t.Fatalf("Doctor after corruption: %v", err)
	}
	if doctor.Passed {
		t.Fatalf("doctor passed after corruption: %#v", doctor)
	}
}

func gitOutputWithInput(directory string, input []byte, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	command.Stdin = bytes.NewReader(input)
	output, err := command.Output()
	return strings.TrimSpace(string(output)), err
}
