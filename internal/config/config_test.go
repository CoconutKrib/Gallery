package config

import (
	"testing"
)

func TestPathOverlaps(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"/foo/bar", "/foo/bar", true},
		{"/foo/bar", "/foo/bar/baz", true},
		{"/foo/bar/baz", "/foo/bar", true},
		{"/foo/bar", "/foo/baz", false},
		{"/foo/bar", "/foo/barbaz", false}, // not a subdir — prefix trick guard
	}
	for _, tc := range cases {
		got := pathOverlaps(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("pathOverlaps(%q, %q) = %v; want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestValidate_Disabled(t *testing.T) {
	cfg := &Config{
		InternalLibrary: InternalLibraryConfig{Enabled: false, Path: "/lib"},
		LibraryPaths:    []LibraryPath{{Path: "/lib"}},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected no error when internal library disabled, got: %v", err)
	}
}

func TestValidate_NoOverlap(t *testing.T) {
	cfg := &Config{
		InternalLibrary: InternalLibraryConfig{Enabled: true, Path: "/gallery"},
		LibraryPaths:    []LibraryPath{{Path: "/photos"}},
		Dropzone:        DropzoneConfig{Enabled: true, Path: "/dropzone"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected no error for non-overlapping paths, got: %v", err)
	}
}

func TestValidate_LibraryPathOverlapsInternal(t *testing.T) {
	cfg := &Config{
		InternalLibrary: InternalLibraryConfig{Enabled: true, Path: "/gallery"},
		LibraryPaths:    []LibraryPath{{Path: "/gallery/scans"}},
	}
	if err := Validate(cfg); err == nil {
		t.Error("expected error when library path is inside internal library path")
	}
}

func TestValidate_InternalOverlapsLibraryPath(t *testing.T) {
	cfg := &Config{
		InternalLibrary: InternalLibraryConfig{Enabled: true, Path: "/photos/library"},
		LibraryPaths:    []LibraryPath{{Path: "/photos"}},
	}
	if err := Validate(cfg); err == nil {
		t.Error("expected error when internal library is inside a library path")
	}
}

func TestValidate_DropzoneOverlapsInternal(t *testing.T) {
	cfg := &Config{
		InternalLibrary: InternalLibraryConfig{Enabled: true, Path: "/gallery"},
		LibraryPaths:    []LibraryPath{{Path: "/photos"}},
		Dropzone:        DropzoneConfig{Enabled: true, Path: "/gallery/dropzone"},
	}
	if err := Validate(cfg); err == nil {
		t.Error("expected error when dropzone path is inside internal library path")
	}
}

func TestValidate_DropzoneDisabled_NoError(t *testing.T) {
	// Dropzone disabled: path overlap should be ignored.
	cfg := &Config{
		InternalLibrary: InternalLibraryConfig{Enabled: true, Path: "/gallery"},
		LibraryPaths:    []LibraryPath{{Path: "/photos"}},
		Dropzone:        DropzoneConfig{Enabled: false, Path: "/gallery"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected no error when dropzone disabled, got: %v", err)
	}
}
