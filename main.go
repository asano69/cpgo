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
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("cpgo", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: cpgo [flags] <src>... <dst>\n\n")
		fmt.Fprintf(os.Stderr, "Behaves like `cp -dR --preserve=all`, always. If <src> is a directory, it\n")
		fmt.Fprintf(os.Stderr, "is copied recursively: into <dst>/<basename of src> if <dst> already exists\n")
		fmt.Fprintf(os.Stderr, "as a directory, or to <dst> itself otherwise. If <src> is a single file, it\n")
		fmt.Fprintf(os.Stderr, "is copied into <dst> if <dst> is a directory, or to the exact path <dst>\n")
		fmt.Fprintf(os.Stderr, "otherwise. If more than one <src> is given, <dst> must already exist as a\n")
		fmt.Fprintf(os.Stderr, "directory, and each source is copied into it. Every copy is verified by\n")
		fmt.Fprintf(os.Stderr, "checksum, with no way to disable it.\n\n")
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
	if fs.NArg() < 2 {
		fs.Usage()
		return 2
	}

	nSrcs := fs.NArg() - 1
	srcs := make([]string, nSrcs)
	for i := 0; i < nSrcs; i++ {
		srcs[i] = filepath.Clean(fs.Arg(i))
	}
	// A trailing slash on the raw dst argument means "this must be a
	// directory", matching `cp`: e.g. `cp file dst/` fails if dst doesn't
	// exist, rather than creating a regular file named dst. filepath.Clean
	// strips the slash, so the flag has to be captured before cleaning.
	rawDst := fs.Arg(nSrcs)
	dstTrailingSlash := strings.HasSuffix(rawDst, "/")
	dst := filepath.Clean(rawDst)

	// Like `cp`, giving more than one source requires dst to already exist
	// as a directory -- there's nowhere else for more than one source to
	// land.
	if nSrcs > 1 {
		dstInfo, err := os.Stat(dst)
		if err != nil || !dstInfo.IsDir() {
			fmt.Fprintf(os.Stderr, "cpgo: target %s is not a directory\n", dst)
			return 1
		}
	}

	opts := Options{
		DryRun:  *dryRun,
		InPlace: *inPlace,
		Jobs:    *jobs,
		Retries: *retries,
		Verbose: *verbose,
	}

	// Like `cp`, a failure on one source doesn't stop the others from being
	// attempted; the overall exit code just ends up non-zero.
	exitCode := 0
	for _, src := range srcs {
		if err := copySrc(src, dst, dstTrailingSlash, opts); err != nil {
			logger.Error(err)
			exitCode = 1
		}
	}
	return exitCode
}

// copySrc copies a single src (file or directory) into/to dst, dispatching
// to directory or single-file mode. Used for every source, whether cpgo was
// given just one or several.
func copySrc(src, dst string, dstTrailingSlash bool, opts Options) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	switch {
	case info.IsDir():
		// A trailing slash on dst doesn't constrain directory-mode copies:
		// `cp -R` happily creates dst as a fresh directory either way.
		return runSync(src, resolveDestDir(src, dst), opts)
	case info.Mode().IsRegular():
		return runSyncSingleFile(src, dst, dstTrailingSlash, opts)
	default:
		return fmt.Errorf("%s: unsupported file type", src)
	}
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
