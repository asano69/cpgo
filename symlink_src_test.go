package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopySrc_SymlinkToDirectory_CopiesLinkNotContents checks that cp -a's
// -d (no-dereference) behavior is honored when src, given directly on the
// command line, is a symlink pointing at a directory: it must be copied as
// a symlink, not followed and recursively copied as if it were a real
// directory.
func TestCopySrc_SymlinkToDirectory_CopiesLinkNotContents(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "realdir")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := copySrc(link, dst, false, Options{Jobs: 1}); err != nil {
		t.Fatalf("copySrc: %v", err)
	}

	destPath := filepath.Join(dst, "link")
	fi, err := os.Lstat(destPath)
	if err != nil {
		t.Fatalf("Lstat(%s): %v", destPath, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("destination %s is not a symlink, want it copied as one", destPath)
	}
	if _, err := os.Lstat(filepath.Join(dst, "f.txt")); !os.IsNotExist(err) {
		t.Error("directory contents should not have been copied directly under dst")
	}
}

// TestCopySrc_DanglingSymlink_IsCopied checks that a broken symlink given
// directly as src -- unremarkable in archival use -- is copied like cp -a
// does, instead of erroring out because os.Stat can't follow it.
func TestCopySrc_DanglingSymlink_IsCopied(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), link); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := copySrc(link, dst, false, Options{Jobs: 1}); err != nil {
		t.Fatalf("copySrc: %v", err)
	}

	destPath := filepath.Join(dst, "dangling")
	fi, err := os.Lstat(destPath)
	if err != nil {
		t.Fatalf("Lstat(%s): %v", destPath, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("destination %s is not a symlink", destPath)
	}
}
