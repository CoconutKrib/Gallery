package scan

import (
	"regexp"
	"testing"
)

// makeScanner is a helper that builds a Scanner with inline include/exclude patterns
// (already compiled with caseInsensitivePattern) for filter testing.
func makeFilterScanner(includes, excludes []string) *Scanner {
	s := &Scanner{}
	for _, p := range includes {
		s.includeRe = append(s.includeRe, regexp.MustCompile(caseInsensitivePattern(p)))
	}
	for _, p := range excludes {
		s.excludeRe = append(s.excludeRe, regexp.MustCompile(caseInsensitivePattern(p)))
	}
	return s
}

func TestPassesFilenameFilters_NoRules(t *testing.T) {
	s := makeFilterScanner(nil, nil)
	for _, name := range []string{"IMG_0001.JPG", "photo.jpg", "DSCN0042.JPEG"} {
		if !s.passesFilenameFilters(name) {
			t.Errorf("expected %q to pass with no rules", name)
		}
	}
}

func TestPassesFilenameFilters_Include_CaseInsensitive(t *testing.T) {
	s := makeFilterScanner([]string{`^dscn`}, nil)
	pass := []string{"DSCN0042.JPG", "dscn0001.jpg", "DsCN_hello.jpeg"}
	fail := []string{"IMG_0001.JPG", "photo.jpg"}

	for _, name := range pass {
		if !s.passesFilenameFilters(name) {
			t.Errorf("expected %q to pass include filter", name)
		}
	}
	for _, name := range fail {
		if s.passesFilenameFilters(name) {
			t.Errorf("expected %q to be rejected by include filter", name)
		}
	}
}

func TestPassesFilenameFilters_Exclude_CaseInsensitive(t *testing.T) {
	s := makeFilterScanner(nil, []string{`^thumb_`})
	pass := []string{"IMG_0001.JPG", "photo.jpg"}
	fail := []string{"thumb_IMG.jpg", "THUMB_001.JPG", "Thumb_copy.jpeg"}

	for _, name := range pass {
		if !s.passesFilenameFilters(name) {
			t.Errorf("expected %q to pass", name)
		}
	}
	for _, name := range fail {
		if s.passesFilenameFilters(name) {
			t.Errorf("expected %q to be rejected by exclude filter", name)
		}
	}
}

func TestPassesFilenameFilters_ExcludeBeatsInclude(t *testing.T) {
	// Include: names starting with IMG; Exclude: names ending with _copy.jpg.
	// A file matching BOTH should be rejected (exclude wins).
	s := makeFilterScanner([]string{`^IMG`}, []string{`_copy\.jpe?g$`})

	// Passes include, not excluded.
	if !s.passesFilenameFilters("IMG_0001.JPG") {
		t.Error("IMG_0001.JPG should pass")
	}
	// Matches include AND exclude — exclude must win.
	if s.passesFilenameFilters("IMG_0001_copy.jpeg") {
		t.Error("IMG_0001_copy.jpeg matches exclude; should be rejected even though it matches include")
	}
	if s.passesFilenameFilters("IMG_0001_COPY.JPEG") {
		t.Error("IMG_0001_COPY.JPEG matches exclude (case-insensitive); should be rejected")
	}
	// Doesn't match include at all.
	if s.passesFilenameFilters("DSCN0042.JPG") {
		t.Error("DSCN0042.JPG doesn't match include; should be rejected")
	}
}

func TestPassesFilenameFilters_ExtensionCaseInsensitive(t *testing.T) {
	// Exclude .jpg extension — should also reject .JPG, .Jpg.
	s := makeFilterScanner(nil, []string{`\.jpg$`})
	fail := []string{"photo.jpg", "PHOTO.JPG", "Photo.Jpg"}
	pass := []string{"photo.jpeg", "IMG.PNG"}

	for _, name := range fail {
		if s.passesFilenameFilters(name) {
			t.Errorf("expected %q to be excluded by .\\.jpg$ pattern", name)
		}
	}
	for _, name := range pass {
		if !s.passesFilenameFilters(name) {
			t.Errorf("expected %q to pass", name)
		}
	}
}

func TestCaseInsensitivePattern(t *testing.T) {
	// Already has (?i) — should not double-wrap.
	if got := caseInsensitivePattern("(?i)foo"); got != "(?i)foo" {
		t.Errorf("expected no double-wrap, got %q", got)
	}
	// Plain pattern — should get (?i) prepended.
	if got := caseInsensitivePattern("foo"); got != "(?i)foo" {
		t.Errorf("expected (?i)foo, got %q", got)
	}
}

func TestPassesFilenameFilters_MultipleExcludes(t *testing.T) {
	// Three independent exclude criteria applied in succession:
	//   1. no dot-files
	//   2. no files starting with "thumb_"
	//   3. no files ending with "_copy.jpg"
	s := makeFilterScanner(nil, []string{`^\.`, `^thumb_`, `_copy\.jpe?g$`})

	pass := []string{"IMG_0001.JPG", "DSCN0042.jpg", "photo.jpeg"}
	fail := map[string]string{
		".hidden.jpg":        "dot-file",
		"thumb_IMG.jpg":      "thumb_ prefix",
		"THUMB_001.JPG":      "thumb_ prefix (case-insensitive)",
		"IMG_0001_copy.jpg":  "_copy.jpg suffix",
		"IMG_0001_COPY.JPEG": "_copy.jpeg suffix (case-insensitive)",
	}

	for _, name := range pass {
		if !s.passesFilenameFilters(name) {
			t.Errorf("expected %q to pass all exclude filters", name)
		}
	}
	for name, reason := range fail {
		if s.passesFilenameFilters(name) {
			t.Errorf("expected %q to be rejected (%s)", name, reason)
		}
	}
}

func TestPassesFilenameFilters_MultipleIncludes(t *testing.T) {
	// Accept files from two different camera naming conventions (either is fine).
	s := makeFilterScanner([]string{`^IMG_`, `^DSCN`}, nil)

	pass := []string{"IMG_0001.JPG", "img_0042.jpeg", "DSCN0001.JPG", "dscn_photo.jpg"}
	fail := []string{"photo.jpg", "2024-01-01.jpg", "MVI_0001.JPG"}

	for _, name := range pass {
		if !s.passesFilenameFilters(name) {
			t.Errorf("expected %q to pass (matches one of the include patterns)", name)
		}
	}
	for _, name := range fail {
		if s.passesFilenameFilters(name) {
			t.Errorf("expected %q to be rejected (matches none of the include patterns)", name)
		}
	}
}
