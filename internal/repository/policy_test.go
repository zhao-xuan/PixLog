package repository

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pixlog/pixlog/internal/imaging"
)

func TestCheckPolicyReportsAndClearsViolations(t *testing.T) {
	root := t.TempDir()
	repo, err := Init(root, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	asset := filepath.Join(root, "hero.png")
	writePolicyPNG(t, asset, image.Point{X: -1, Y: -1})
	if _, err := repo.Add([]string{asset}, ""); err != nil {
		t.Fatalf("Add base: %v", err)
	}
	if _, _, err := repo.CreateCommit("base", "tester", time.Unix(1, 0)); err != nil {
		t.Fatalf("CreateCommit: %v", err)
	}

	writePolicyPNG(t, asset, image.Point{X: 3, Y: 3})
	if _, err := repo.Add([]string{asset}, ""); err != nil {
		t.Fatalf("Add changed: %v", err)
	}
	deniedMask := filepath.Join(root, "deny-mask.png")
	writePolicyMask(t, deniedMask, image.Point{X: 0, Y: 0})

	tooSmall := int64(1)
	maxChange := 0.5
	failingPolicy := Policy{
		Schema: PolicySchema,
		Rules: []PolicyRule{{
			Asset:                "hero.png",
			AllowedFormats:       []string{"jpeg"},
			MaxFileSize:          &tooSmall,
			RequireRecipe:        true,
			MaxVisualChange:      &maxChange,
			AllowedChangeRegions: []imaging.BoundingBox{{X: 0, Y: 0, Width: 2, Height: 2}},
			AllowedChangeMask:    "deny-mask.png",
		}},
	}
	policyPath := filepath.Join(root, ".pixlog-policy.json")
	writePolicyFile(t, policyPath, failingPolicy)
	result, err := repo.CheckPolicy(policyPath, imaging.DiffOptions{Threshold: 0})
	if err != nil {
		t.Fatalf("CheckPolicy failing: %v", err)
	}
	if result.Passed {
		t.Fatal("failing policy unexpectedly passed")
	}
	wantCodes := map[string]bool{
		"format_not_allowed":            false,
		"file_too_large":                false,
		"recipe_required":               false,
		"change_outside_allowed_region": false,
		"change_outside_allowed_mask":   false,
	}
	for _, violation := range result.Violations {
		if _, expected := wantCodes[violation.Code]; expected {
			wantCodes[violation.Code] = true
		}
	}
	for code, found := range wantCodes {
		if !found {
			t.Errorf("missing violation %s in %#v", code, result.Violations)
		}
	}

	largeEnough := int64(1 << 20)
	minSSIM := 0.01
	allowedMask := filepath.Join(root, "allow-mask.png")
	writePolicyMask(t, allowedMask, image.Point{X: 3, Y: 3})
	passingPolicy := Policy{
		Schema: PolicySchema,
		Rules: []PolicyRule{{
			Asset:                "hero.png",
			AllowedFormats:       []string{"png"},
			MaxFileSize:          &largeEnough,
			MaxVisualChange:      &maxChange,
			MinSSIM:              &minSSIM,
			AllowedChangeRegions: []imaging.BoundingBox{{X: 3, Y: 3, Width: 1, Height: 1}},
			AllowedChangeMask:    "allow-mask.png",
		}},
	}
	writePolicyFile(t, policyPath, passingPolicy)
	result, err = repo.CheckPolicy(policyPath, imaging.DiffOptions{Threshold: 0})
	if err != nil {
		t.Fatalf("CheckPolicy passing: %v", err)
	}
	if !result.Passed || len(result.Violations) != 0 {
		t.Fatalf("passing policy result: %#v", result)
	}
}

func TestVisualPolicyDoesNotRequireRegionsUnlessConfigured(t *testing.T) {
	maxChange := 0.5
	asset := AssetDiff{
		Path: "hero.png",
		Old:  &Entry{ContentOID: "sha256:old"},
		New:  &Entry{ContentOID: "sha256:new"},
		Visual: &imaging.VisualDiff{
			Comparable:        true,
			VisualChangeRatio: 0.1,
			SSIM:              0.95,
			Regions: []imaging.ChangedRegion{{
				BoundingBox: imaging.BoundingBox{X: 10, Y: 20, Width: 30, Height: 40},
				Pixels:      100,
			}},
		},
	}
	violations := checkVisualPolicy(0, PolicyRule{Asset: "hero.png", MaxVisualChange: &maxChange}, asset)
	if len(violations) != 0 {
		t.Fatalf("metric-only policy violations = %#v", violations)
	}
}

func writePolicyPNG(t *testing.T, filePath string, changed image.Point) {
	t.Helper()
	output := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			pixel := color.NRGBA{R: 20, G: 40, B: 60, A: 255}
			if x == changed.X && y == changed.Y {
				pixel = color.NRGBA{R: 240, G: 220, B: 200, A: 255}
			}
			output.SetNRGBA(x, y, pixel)
		}
	}
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("create PNG: %v", err)
	}
	if err := png.Encode(file, output); err != nil {
		file.Close()
		t.Fatalf("encode PNG: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close PNG: %v", err)
	}
}

func writePolicyMask(t *testing.T, filePath string, allowed image.Point) {
	t.Helper()
	output := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	output.SetNRGBA(allowed.X, allowed.Y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("create mask: %v", err)
	}
	if err := png.Encode(file, output); err != nil {
		file.Close()
		t.Fatalf("encode mask: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close mask: %v", err)
	}
}

func writePolicyFile(t *testing.T, filePath string, policy Policy) {
	t.Helper()
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
}
