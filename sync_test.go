package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCopyVerified_Success checks the happy path: a plain copy lands intact
// and no temp file is left behind.
func TestCopyVerified_Success(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	content := []byte("hello, cpgo")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(dir, "dst.bin")

	n, err := copyVerified(srcPath, destPath, srcInfo, nil)
	if err != nil {
		t.Fatalf("copyVerified: %v", err)
	}
	if n != int64(len(content)) {
		t.Errorf("copied %d bytes, want %d", n, len(content))
	}
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("dest content = %q, want %q", got, content)
	}
	if _, err := os.Stat(destPath + ".cpgo.tmp"); !os.IsNotExist(err) {
		t.Error("temp file should be gone after a successful copy")
	}
}

// TestCopyVerified_DetectsCorruptionDuringWrite simulates a bit getting
// flipped on the way to disk (bad storage, a stray write, etc.) while a copy
// is in flight. The content is made large enough that io.Copy needs more
// than one internal buffer, so by the time we corrupt byte 0 the first chunk
// has already been flushed to the temp file and won't be rewritten. The
// corruption must surface as a checksum mismatch, and no half-good file
// should ever reach the destination path.
func TestCopyVerified_DetectsCorruptionDuringWrite(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	content := bytes.Repeat([]byte("0123456789abcdef"), 8000) // 128000 bytes
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(dir, "dst.bin")
	tmpPath := destPath + ".cpgo.tmp"

	calls := 0
	corruptOnSecondChunk := func(int64) {
		calls++
		if calls != 2 {
			return // let the first chunk land on disk untouched
		}
		f, err := os.OpenFile(tmpPath, os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("opening temp file to corrupt it: %v", err)
		}
		defer f.Close()
		if _, err := f.WriteAt([]byte{'X'}, 0); err != nil {
			t.Fatalf("corrupting temp file: %v", err)
		}
	}

	_, err = copyVerified(srcPath, destPath, srcInfo, corruptOnSecondChunk)
	if err == nil {
		t.Fatal("expected a checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v, want a checksum mismatch error", err)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Error("destination should not exist after a failed copy")
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up after a failed copy")
	}
}

// TestCopyVerified_DetectsSourceChangedSize covers the other half of "the
// source got corrupted/replaced mid-copy": copyVerified is handed FileInfo
// that no longer matches what's actually on disk at srcPath (as if the file
// were truncated or swapped out after being scanned), and must refuse to
// treat the result as good.
func TestCopyVerified_DetectsSourceChangedSize(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(srcPath, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}

	staleSrcPath := filepath.Join(dir, "stale.bin")
	if err := os.WriteFile(staleSrcPath, []byte("this used to be much longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleInfo, err := os.Stat(staleSrcPath)
	if err != nil {
		t.Fatal(err)
	}

	destPath := filepath.Join(dir, "dst.bin")
	_, err = copyVerified(srcPath, destPath, staleInfo, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "source changed size") {
		t.Errorf("error = %v, want a source-changed-size error", err)
	}
}

// TestIsUpToDate_DetectsSilentCorruption checks that a destination file
// which is the right size but has different bytes -- e.g. bit rot that
// happened after a previous, successful run -- is never mistaken for
// up to date. This is the property that makes cpgo self-healing on rerun.
func TestIsUpToDate_DetectsSilentCorruption(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	content := []byte("the file that must not change")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatal(err)
	}

	destPath := filepath.Join(dir, "dst.bin")
	corrupted := bytes.Clone(content)
	corrupted[0] ^= 0xFF
	if err := os.WriteFile(destPath, corrupted, 0o644); err != nil {
		t.Fatal(err)
	}

	up, err := isUpToDate(srcPath, destPath, srcInfo)
	if err != nil {
		t.Fatal(err)
	}
	if up {
		t.Error("isUpToDate = true for a corrupted destination, want false")
	}
}

// TestSyncFile_SelfHealsCorruptedDestination is an end-to-end check of the
// README's resumability claim: a destination corrupted after a first
// successful sync gets repaired by simply running the sync again.
func TestSyncFile_SelfHealsCorruptedDestination(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	content := []byte("data that must survive a resync")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(dir, "dst.bin")
	opts := Options{Retries: 2}

	prog := &Progress{TotalBytes: int64(len(content)), TotalFiles: 1}
	if err := syncFile(srcPath, destPath, srcInfo, opts, prog); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// Corrupt the destination directly, as if a disk had flipped a bit
	// after the first sync finished.
	corrupted := bytes.Clone(content)
	corrupted[len(corrupted)-1] ^= 0xFF
	if err := os.WriteFile(destPath, corrupted, 0o644); err != nil {
		t.Fatal(err)
	}

	prog2 := &Progress{TotalBytes: int64(len(content)), TotalFiles: 1}
	if err := syncFile(srcPath, destPath, srcInfo, opts, prog2); err != nil {
		t.Fatalf("resync: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Error("syncFile did not repair the corrupted destination")
	}
}

// TestSyncFile_FailsAfterRetriesExhausted checks the give-up path: when the
// corruption is persistent (here, FileInfo that never matches the source on
// disk, standing in for a source that's corrupted at every attempt), syncFile
// retries opts.Retries extra times and then reports failure instead of
// silently accepting a bad copy.
func TestSyncFile_FailsAfterRetriesExhausted(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(srcPath, []byte("actual content"), 0o644); err != nil {
		t.Fatal(err)
	}

	stalePath := filepath.Join(dir, "stale.bin")
	if err := os.WriteFile(stalePath, []byte("a very different length of content"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleInfo, err := os.Stat(stalePath)
	if err != nil {
		t.Fatal(err)
	}

	destPath := filepath.Join(dir, "dst.bin")
	opts := Options{Retries: 2}
	prog := &Progress{TotalBytes: staleInfo.Size(), TotalFiles: 1}

	err = syncFile(srcPath, destPath, staleInfo, opts, prog)
	if err == nil {
		t.Fatal("expected an error after retries are exhausted, got nil")
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("error = %v, want it to mention 3 attempts (1 + 2 retries)", err)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Error("destination should not exist after every attempt failed")
	}
}
