package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// checkNotIntoSelf guards against copying a directory into itself or into
// one of its own subdirectories, mirroring `cp -R`'s refusal (e.g. `cp -R
// dir dir/sub` errors instead of recursing forever). dst here is the actual
// resolved destination directory (i.e. the output of resolveDestDir), not
// the raw command-line argument.
func checkNotIntoSelf(src, dst string) error {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	if absSrc == absDst {
		return fmt.Errorf("cannot copy a directory, %s, into itself, %s", src, dst)
	}
	rel, err := filepath.Rel(absSrc, absDst)
	if err != nil {
		return nil // unrelated paths (e.g. different drives): nothing to guard against
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("cannot copy a directory, %s, into itself, %s", src, dst)
	}
	return nil
}

// checkNotSameFile guards against copying a regular file onto itself, e.g.
// `cpgo file file` or `cpgo file link-to-file`, mirroring cp's "are the
// same file" error. Lexical equality catches the common case even before
// destPath exists; the stat-based inode check also catches distinct paths
// (a hardlink, or the same file reached via a different route) that
// resolve to the same file on disk.
func checkNotSameFile(src, destPath string) error {
	if absSrc, err := filepath.Abs(src); err == nil {
		if absDest, err := filepath.Abs(destPath); err == nil && absSrc == absDest {
			return fmt.Errorf("%s and %s are the same file", src, destPath)
		}
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return nil // src stat problems are reported by the caller
	}
	dstInfo, err := os.Stat(destPath)
	if err != nil {
		return nil // destination doesn't exist yet: can't be the same file
	}
	if os.SameFile(srcInfo, dstInfo) {
		return fmt.Errorf("%s and %s are the same file", src, destPath)
	}
	return nil
}
