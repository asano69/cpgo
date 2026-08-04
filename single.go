package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// runSyncSingleFile copies one regular file to dst, using the same
// checksum-verified, resumable copy logic as tree sync. If dst is an
// existing directory, the file is copied into it under its original name
// (like `cp src dir/`); otherwise dst is treated as the destination file's
// exact path. dstTrailingSlash records whether the dst argument, before
// path cleaning, ended in "/" -- like `cp`, that forces dst to be treated
// as a directory, and it's an error if it isn't one.
func runSyncSingleFile(src, dst string, dstTrailingSlash bool, opts Options) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}

	dstInfo, statErr := os.Stat(dst)
	dstIsDir := statErr == nil && dstInfo.IsDir()

	if dstTrailingSlash && !dstIsDir {
		return fmt.Errorf("cannot create regular file %s/: not a directory", dst)
	}

	destPath := dst
	if dstIsDir {
		destPath = filepath.Join(dst, filepath.Base(src))
	}

	// Like `cp`, this never creates the destination directory: it's an
	// error for the directory destPath would land in to not already exist.
	destDir := filepath.Dir(destPath)
	if info, err := os.Stat(destDir); err != nil || !info.IsDir() {
		return fmt.Errorf("cannot create regular file %s: no such directory %s", destPath, destDir)
	}

	prog := &Progress{TotalBytes: srcInfo.Size(), TotalFiles: 1}
	stopProgress := startProgressPrinter(prog, opts)
	defer stopProgress()

	syncErr := syncFile(src, destPath, srcInfo, opts, prog)
	prog.DoneFiles.Add(1)
	if syncErr != nil {
		handleFileSyncError(prog, filepath.Base(src), syncErr)
	}

	stopProgress()
	printFinalSummary(prog)

	if prog.Failed.Load() > 0 {
		return fmt.Errorf("copy failed")
	}
	return nil
}
