package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckNotIntoSelf_SameDirectory checks the exact-match case: copying a
// directory onto itself is refused, just like `cp -R dir dir`.
func TestCheckNotIntoSelf_SameDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := checkNotIntoSelf(dir, dir); err == nil {
		t.Fatal("expected an error when dst is the same directory as src")
	}
}

// TestCheckNotIntoSelf_DestinationNestedInsideSource checks the more common
// real-world trap: `cpgo dir dir/backup`, where dst lands inside src.
func TestCheckNotIntoSelf_DestinationNestedInsideSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photos")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(src, "backup")

	if err := checkNotIntoSelf(src, nested); err == nil {
		t.Fatal("expected an error when dst is nested inside src")
	}
}

// TestCheckNotIntoSelf_UnrelatedDestinationIsFine checks the common, valid
// case isn't accidentally rejected: dst that has nothing to do with src.
func TestCheckNotIntoSelf_UnrelatedDestinationIsFine(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photos")
	dst := filepath.Join(dir, "backup")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := checkNotIntoSelf(src, dst); err != nil {
		t.Errorf("checkNotIntoSelf(%q, %q) = %v, want nil", src, dst, err)
	}
}

// TestCheckNotSameFile_IdenticalPath checks the trivial case: src and dst
// are literally the same path.
func TestCheckNotSameFile_IdenticalPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := checkNotSameFile(path, path); err == nil {
		t.Fatal("expected an error when src and dst are the same path")
	}
}

// TestCheckNotSameFile_SameInodeViaHardlink checks the case the lexical
// check alone would miss: two different paths that are hardlinks to the
// same underlying file.
func TestCheckNotSameFile_SameInodeViaHardlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias.bin")
	if err := os.Link(src, link); err != nil {
		t.Fatal(err)
	}

	if err := checkNotSameFile(src, link); err == nil {
		t.Fatal("expected an error when src and dst are hardlinks to the same file")
	}
}

// TestCheckNotSameFile_DifferentFilesAreFine checks that ordinary, distinct
// files are never mistaken for the same file.
func TestCheckNotSameFile_DifferentFilesAreFine(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.bin")
	dst := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := checkNotSameFile(src, dst); err != nil {
		t.Errorf("checkNotSameFile(%q, %q) = %v, want nil", src, dst, err)
	}
}
