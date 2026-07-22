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
// exact path.
func runSyncSingleFile(src, dst string, opts Options) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}

	destPath := dst
	if dstInfo, err := os.Stat(dst); err == nil && dstInfo.IsDir() {
		destPath = filepath.Join(dst, filepath.Base(src))
	}

	if !opts.DryRun {
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("creating destination directory: %w", err)
		}
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
