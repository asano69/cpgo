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

	err := runSyncSingleFile(srcPath, destPath, Options{})
	if err == nil {
		t.Fatal("expected an error when the destination directory does not exist, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "no-such-dir")); !os.IsNotExist(statErr) {
		t.Error("destination directory should not have been created")
	}
}
