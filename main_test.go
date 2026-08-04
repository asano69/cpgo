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

// TestRun_MultipleSources_CopiesEachIntoDestinationDirectory checks the
// `cp`-style multi-source form: cpgo a b c dst copies each of a, b, c into
// dst, which must already exist as a directory.
func TestRun_MultipleSources_CopiesEachIntoDestinationDirectory(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src1, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	src2 := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(src2, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{src1, src2, dst}); code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("%s missing in destination: %v", name, err)
		}
	}
}

// TestRun_MultipleSources_ErrorsWhenDestinationNotDirectory checks that,
// like `cp`, giving more than one source requires dst to already exist as a
// directory -- it's an error, not something cpgo creates on the fly.
func TestRun_MultipleSources_ErrorsWhenDestinationNotDirectory(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src1, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	src2 := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(src2, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst.txt") // not a directory, and not created

	if code := run([]string{src1, src2, dst}); code == 0 {
		t.Fatal("run() = 0, want nonzero when dst is not a directory with multiple sources")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("destination should not have been created")
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
