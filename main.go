// Command cpgo mirrors one directory tree onto another, verifying every
// copied file by re-reading and re-hashing it from disk, and resuming
// cleanly if interrupted.
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
		fmt.Fprintf(os.Stderr, "Mirrors the contents of <src> into <dst>: copies what's missing or\n")
		fmt.Fprintf(os.Stderr, "changed, verifies every copy by checksum, and (by default) removes\n")
		fmt.Fprintf(os.Stderr, "anything in <dst> that no longer exists in <src>.\n\n")
		fs.PrintDefaults()
	}

	noDelete := fs.Bool("no-delete", false, "don't delete files in <dst> that are missing from <src>")
	checksumAll := fs.Bool("checksum", false, "also verify already-present files by content hash, not just size+mtime")
	dryRun := fs.Bool("dry-run", false, "show what would be done without changing anything")
	jobs := fs.Int("jobs", runtime.NumCPU(), "number of files to copy concurrently")
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
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "cpgo: %s is not a directory\n", src)
		return 1
	}

	opts := Options{
		Delete:      !*noDelete,
		ChecksumAll: *checksumAll,
		DryRun:      *dryRun,
		Jobs:        *jobs,
		Retries:     *retries,
		Verbose:     *verbose,
	}

	if err := runSync(src, dst, opts); err != nil {
		fmt.Fprintf(os.Stderr, "cpgo: %v\n", err)
		return 1
	}
	return 0
}
