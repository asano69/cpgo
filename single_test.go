package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunSyncSingleFile_ErrorsWhenDestDirMissing checks that, like `cp`,
// runSyncSingleFile never creates a missing destination directory -- it's
// an error for the directory dst would land in to not already exist.
func TestRunSyncSingleFile_ErrorsWhenDestDirMissing(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(srcPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(dir, "no-such-dir", "dst.bin")

	err := runSyncSingleFile(srcPath, destPath, false, Options{})
	if err == nil {
		t.Fatal("expected an error when the destination directory does not exist, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "no-such-dir")); !os.IsNotExist(statErr) {
		t.Error("destination directory should not have been created")
	}
}

// TestRunSyncSingleFile_TrailingSlashRequiresExistingDirectory checks `cp`'s
// trailing-slash rule: `cpgo file dst/` demands dst already be a directory,
// and errors out instead of creating a regular file named dst when it isn't.
func TestRunSyncSingleFile_TrailingSlashRequiresExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(srcPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "no-such-dir") // does not exist

	err := runSyncSingleFile(srcPath, dst, true, Options{})
	if err == nil {
		t.Fatal("expected an error when dst has a trailing slash but is not an existing directory, got nil")
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Error("destination should not have been created as a regular file")
	}
}

// TestRunSyncSingleFile_TrailingSlashIntoExistingDirectory checks the
// success case of the same rule: when dst does exist as a directory, a
// trailing slash makes no difference and the file is copied into it,
// exactly as without the slash.
func TestRunSyncSingleFile_TrailingSlashIntoExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(srcPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := runSyncSingleFile(srcPath, dst, true, Options{}); err != nil {
		t.Fatalf("runSyncSingleFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "src.bin")); err != nil {
		t.Errorf("file missing in destination: %v", err)
	}
}
