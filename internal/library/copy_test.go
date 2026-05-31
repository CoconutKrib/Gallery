package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Wedding Smith", "Wedding-Smith"},
		{"Holiday Cornwall 2018", "Holiday-Cornwall-2018"},
		{"  lots   of   spaces  ", "Lots-Of-Spaces"},
		{"café & brûlé!", "Caf-Br-L"}, // non-ASCII chars stripped by [^a-zA-Z0-9]+ regex
		{"already-hyphenated", "Already-Hyphenated"},
		{"123 numbers", "123-Numbers"},
		{"", ""},
		{"---", ""},
		{"A", "A"},
	}
	for _, tc := range cases {
		got := slugify(tc.input)
		if got != tc.want {
			t.Errorf("slugify(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveFilename_NoCollision(t *testing.T) {
	dir := t.TempDir()
	got := resolveFilename("IMG_0001.jpg", "aabbccdd1122", dir)
	if got != "IMG_0001.jpg" {
		t.Errorf("expected unchanged filename, got %q", got)
	}
}

func TestResolveFilename_Collision(t *testing.T) {
	dir := t.TempDir()
	// Create a file that collides.
	if err := os.WriteFile(filepath.Join(dir, "IMG_0001.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveFilename("IMG_0001.jpg", "aabbccdd1122", dir)
	want := "IMG_0001_aabbccdd.jpg"
	if got != want {
		t.Errorf("resolveFilename collision = %q; want %q", got, want)
	}
}

func TestResolveFilename_ShortHash(t *testing.T) {
	dir := t.TempDir()
	// Create colliding file with a short sha256 (< 8 chars).
	if err := os.WriteFile(filepath.Join(dir, "scan.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveFilename("scan.jpg", "abc", dir)
	want := "scan_abc.jpg"
	if got != want {
		t.Errorf("resolveFilename short hash = %q; want %q", got, want)
	}
}

func TestBuildRelDir_Dated(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	got := buildRelDir(&ts, false, nil, nil)
	want := "2024/06"
	if got != want {
		t.Errorf("buildRelDir dated = %q; want %q", got, want)
	}
}

func TestBuildRelDir_Undated(t *testing.T) {
	got := buildRelDir(nil, true, nil, nil)
	if got != "_undated" {
		t.Errorf("buildRelDir undated = %q; want _undated", got)
	}
}

func TestBuildRelDir_TrueDateUnknown(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	// Even with a date, trueDateUnknown forces _undated.
	got := buildRelDir(&ts, true, nil, nil)
	if got != "_undated" {
		t.Errorf("buildRelDir trueDateUnknown = %q; want _undated", got)
	}
}
