package repository

import (
	"reflect"
	"testing"
)

func TestCompareAssetSnapshots(t *testing.T) {
	before := map[string]string{
		"deleted.png": "sha256:old-deleted",
		"same.png":    "sha256:same",
		"updated.png": "sha256:old",
	}
	after := map[string]string{
		"added.png":   "sha256:new-added",
		"same.png":    "sha256:same",
		"updated.png": "sha256:new",
	}
	delta := CompareAssetSnapshots(before, after)
	if want := []string{"added.png", "updated.png"}; !reflect.DeepEqual(delta.Modified, want) {
		t.Fatalf("Modified = %v, want %v", delta.Modified, want)
	}
	if want := []string{"deleted.png"}; !reflect.DeepEqual(delta.Deleted, want) {
		t.Fatalf("Deleted = %v, want %v", delta.Deleted, want)
	}
}
