package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// resolveSingleDest applies `cp`'s destination-path rule for a single
// filesystem entry (regular file or symlink) named src: if dst already
// exists as a directory, src lands inside it under its own base name;
// otherwise dst is treated as the exact destination path. dstTrailingSlash
// records whether the dst argument, before path cleaning, ended in "/" --
// like `cp`, that forces dst to be treated as a directory, and it's an
// error if it isn't one.
func resolveSingleDest(src, dst string, dstTrailingSlash bool) (string, error) {
	dstInfo, statErr := os.Stat(dst)
	dstIsDir := statErr == nil && dstInfo.IsDir()

	if dstTrailingSlash && !dstIsDir {
		return "", fmt.Errorf("cannot create %s/: not a directory", dst)
	}

	destPath := dst
	if dstIsDir {
		destPath = filepath.Join(dst, filepath.Base(src))
	}
	return destPath, nil
}

// runSyncSingleFile copies one regular file to dst, using the same
// checksum-verified, resumable copy logic as tree sync.
func runSyncSingleFile(src, dst string, dstTrailingSlash bool, opts Options) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}

	destPath, err := resolveSingleDest(src, dst, dstTrailingSlash)
	if err != nil {
		return err
	}

	if err := checkNotSameFile(src, destPath); err != nil {
		return err
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

// runSyncSingleSymlink copies a single symlink to dst without following it,
// matching `cp -a`'s -d (no-dereference) behavior when SOURCE given
// directly on the command line is itself a symlink. srcInfo must come from
// Lstat, not Stat, so this also works for a dangling (broken) symlink,
// which archival use has to tolerate.
func runSyncSingleSymlink(src, dst string, dstTrailingSlash bool, opts Options, srcInfo fs.FileInfo) error {
	destPath, err := resolveSingleDest(src, dst, dstTrailingSlash)
	if err != nil {
		return err
	}

	if err := checkNotSameFile(src, destPath); err != nil {
		return err
	}

	// Like `cp`, this never creates the destination directory: it's an
	// error for the directory destPath would land in to not already exist.
	destDir := filepath.Dir(destPath)
	if info, err := os.Stat(destDir); err != nil || !info.IsDir() {
		return fmt.Errorf("cannot create symlink %s: no such directory %s", destPath, destDir)
	}

	prog := &Progress{TotalFiles: 1}
	stopProgress := startProgressPrinter(prog, opts)
	defer stopProgress()

	if err := syncSymlink(src, destPath, srcInfo, opts); err != nil {
		warnAndCountFailure(prog, err, "symlink %s", src)
	}
	prog.DoneFiles.Add(1)

	stopProgress()
	printFinalSummary(prog)

	if prog.Failed.Load() > 0 {
		return fmt.Errorf("copy failed")
	}
	return nil
}
