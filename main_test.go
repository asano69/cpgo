package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveDestDir_NestsIntoExistingDirectory checks the `cp -R`-style
// destination resolution: when dst already exists as a directory, src is
// copied *into* it (dst/basename(src)), not merged into dst's contents.
func TestResolveDestDir_NestsIntoExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photos")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "backup")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveDestDir(src, dst)
	want := filepath.Join(dst, "photos")
	if got != want {
		t.Errorf("resolveDestDir(%q, %q) = %q, want %q", src, dst, got, want)
	}
}

// TestResolveDestDir_UsesDstDirectlyWhenItDoesNotExist checks the other
// `cp -R` case: when dst doesn't exist yet, dst itself becomes the copy of
// src, rather than nesting src underneath it.
func TestResolveDestDir_UsesDstDirectlyWhenItDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photos")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "backup") // does not exist yet

	got := resolveDestDir(src, dst)
	if got != dst {
		t.Errorf("resolveDestDir(%q, %q) = %q, want %q", src, dst, got, dst)
	}
}

// TestResolveDestDir_UsesDstDirectlyWhenItIsAFile checks that an existing
// *file* at dst is left as the literal destination and not treated as
// something to nest into -- runSync goes on to fail on it, the same way
// `cp -R` refuses to overwrite a file with a directory, but reporting that
// failure is runSync's job, not resolveDestDir's.
func TestResolveDestDir_UsesDstDirectlyWhenItIsAFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photos")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "backup")
	if err := os.WriteFile(dst, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveDestDir(src, dst)
	if got != dst {
		t.Errorf("resolveDestDir(%q, %q) = %q, want %q", src, dst, got, dst)
	}
}
