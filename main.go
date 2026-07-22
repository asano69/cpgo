// Command cpgo copies files with checksum verification, resuming cleanly if
// interrupted. Given a directory, it mirrors the whole tree; given a single
// file, it copies just that file.
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
		fmt.Fprintf(os.Stderr, "If <src> is a directory, mirrors its contents into <dst>: copies what's\n")
		fmt.Fprintf(os.Stderr, "missing or changed, and (by default) removes anything in <dst> that no\n")
		fmt.Fprintf(os.Stderr, "longer exists in <src>. If <src> is a single file, copies just that file\n")
		fmt.Fprintf(os.Stderr, "(into <dst> if it's a directory, or to the exact path <dst> otherwise).\n")
		fmt.Fprintf(os.Stderr, "Every copy is verified by checksum, with no way to disable it.\n\n")
		fs.PrintDefaults()
	}

	noDelete := fs.Bool("no-delete", false, "don't delete files in <dst> that are missing from <src> (directory mode only)")
	dryRun := fs.Bool("dry-run", false, "show what would be done without changing anything")
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
		fmt.Fprintf(os.Stderr, "cpgo: %v\n", err)
		return 1
	}

	opts := Options{
		Delete:  !*noDelete,
		DryRun:  *dryRun,
		Jobs:    *jobs,
		Retries: *retries,
		Verbose: *verbose,
	}

	switch {
	case info.IsDir():
		err = runSync(src, dst, opts)
	case info.Mode().IsRegular():
		err = runSyncSingleFile(src, dst, opts)
	default:
		fmt.Fprintf(os.Stderr, "cpgo: %s: unsupported file type\n", src)
		return 1
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "cpgo: %v\n", err)
		return 1
	}
	return 0
}
