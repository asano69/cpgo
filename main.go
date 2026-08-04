// Command cpgo copies files with checksum verification, resuming cleanly if
// interrupted. It behaves like `cp -dR --preserve=all`, always: given a
// directory it copies the whole tree recursively, and given a single file
// it copies just that file.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("cpgo", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: cpgo [flags] <src> <dst>\n\n")
		fmt.Fprintf(os.Stderr, "Behaves like `cp -dR --preserve=all`, always. If <src> is a directory, it\n")
		fmt.Fprintf(os.Stderr, "is copied recursively: into <dst>/<basename of src> if <dst> already exists\n")
		fmt.Fprintf(os.Stderr, "as a directory, or to <dst> itself otherwise. If <src> is a single file, it\n")
		fmt.Fprintf(os.Stderr, "is copied into <dst> if <dst> is a directory, or to the exact path <dst>\n")
		fmt.Fprintf(os.Stderr, "otherwise. Every copy is verified by checksum, with no way to disable it.\n\n")
		fs.PrintDefaults()
	}

	dryRun := fs.Bool("dry-run", false, "show what would be done without changing anything")
	inPlace := fs.Bool("in-place", false, "write directly to the destination instead of a temp file + rename; "+
		"uses less spare disk space per file, but a copy that fails or is interrupted midway leaves the "+
		"destination corrupted instead of untouched")
	jobs := fs.Int("jobs", runtime.NumCPU(), "number of files to copy concurrently (directory mode only)")
	retries := fs.Int("retries", 2, "extra attempts if a copy fails checksum verification")
	verbose := fs.Bool("verbose", false, "print each action taken")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return 2
	}

	src := filepath.Clean(fs.Arg(0))
	dst := filepath.Clean(fs.Arg(1))

	info, err := os.Stat(src)
	if err != nil {
		logger.Error(err)
		return 1
	}

	opts := Options{
		DryRun:  *dryRun,
		InPlace: *inPlace,
		Jobs:    *jobs,
		Retries: *retries,
		Verbose: *verbose,
	}

	switch {
	case info.IsDir():
		err = runSync(src, resolveDestDir(src, dst), opts)
	case info.Mode().IsRegular():
		err = runSyncSingleFile(src, dst, opts)
	default:
		fmt.Fprintf(os.Stderr, "cpgo: %s: unsupported file type\n", src)
		return 1
	}

	if err != nil {
		logger.Error(err)
		return 1
	}
	return 0
}

// resolveDestDir applies `cp -R` semantics for a directory source: if dst
// already exists as a directory, src is copied *into* it (dst/basename(src)),
// same as `cp -R src dst` when dst pre-exists. Otherwise dst itself becomes
// the copy of src, same as `cp -R src dst` creating a fresh dst.
func resolveDestDir(src, dst string) string {
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		return filepath.Join(dst, filepath.Base(src))
	}
	return dst
}
