package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
)

// Options controls how the sync runs.
type Options struct {
	Delete  bool // remove destination entries that no longer exist in the source
	DryRun  bool
	Jobs    int // concurrent file copies
	Retries int // extra attempts after a checksum mismatch
	Verbose bool
}

// Progress is shared, atomically-updated state used to render the progress line.
type Progress struct {
	TotalBytes int64
	DoneBytes  atomic.Int64
	TotalFiles int64
	DoneFiles  atomic.Int64
	Failed     atomic.Int64
}

// runSync mirrors src into dst according to opts, printing progress as it goes.
func runSync(src, dst string, opts Options) error {
	tree, err := scanTree(src)
	if err != nil {
		return fmt.Errorf("scanning source: %w", err)
	}

	var totalFiles int64
	for _, e := range tree.Entries {
		if !e.IsDir {
			totalFiles++
		}
	}
	prog := &Progress{TotalBytes: tree.TotalBytes, TotalFiles: totalFiles}

	stopProgress := startProgressPrinter(prog, opts)
	defer stopProgress()

	if !opts.DryRun {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dst, err)
		}
	}

	// Pass A: create every directory first, so files and symlinks always have
	// somewhere to land. Attributes are fixed up afterwards in pass C, since
	// writing into a directory changes its mtime.
	for _, e := range tree.Entries {
		if !e.IsDir {
			continue
		}
		destPath := filepath.Join(dst, filepath.FromSlash(e.RelPath))
		if opts.DryRun {
			continue
		}
		if err := os.MkdirAll(destPath, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", destPath, err)
		}
	}

	// Pass B1: copy regular files and create symlinks, concurrently for files.
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = 1
	}
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup

	for _, e := range tree.Entries {
		e := e
		if e.IsDir {
			continue
		}
		if _, isSecondary := tree.HardlinkOf[e.RelPath]; isSecondary {
			continue // handled in pass B2, after its primary is copied
		}
		srcPath := e.AbsPath
		destPath := filepath.Join(dst, filepath.FromSlash(e.RelPath))

		if e.IsSymlink {
			if err := syncSymlink(srcPath, destPath, e.Info, opts); err != nil {
				fmt.Fprintf(os.Stderr, "cpgo: symlink %s: %v\n", e.RelPath, err)
				prog.Failed.Add(1)
			}
			prog.DoneFiles.Add(1)
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := syncFile(srcPath, destPath, e.Info, opts, prog); err != nil {
				fmt.Fprintf(os.Stderr, "cpgo: %s: %v\n", e.RelPath, err)
				prog.Failed.Add(1)
			}
			prog.DoneFiles.Add(1)
		}()
	}
	wg.Wait()

	// Pass B2: recreate hardlinks now that every primary file has been copied.
	// Sorting keeps this deterministic; it's cheap so it stays sequential.
	var secondaryPaths []string
	for rel := range tree.HardlinkOf {
		secondaryPaths = append(secondaryPaths, rel)
	}
	sort.Strings(secondaryPaths)
	for _, rel := range secondaryPaths {
		primaryRel := tree.HardlinkOf[rel]
		destPath := filepath.Join(dst, filepath.FromSlash(rel))
		primaryDestPath := filepath.Join(dst, filepath.FromSlash(primaryRel))
		if opts.DryRun {
			continue
		}
		if err := syncHardlink(primaryDestPath, destPath); err != nil {
			fmt.Fprintf(os.Stderr, "cpgo: hardlink %s: %v\n", rel, err)
			prog.Failed.Add(1)
		}
		prog.DoneFiles.Add(1)
	}

	// Pass C: fix up directory attributes, deepest first, so nothing we did
	// above (creating children) disturbs a parent's mtime afterwards.
	for i := len(tree.Entries) - 1; i >= 0; i-- {
		e := tree.Entries[i]
		if !e.IsDir {
			continue
		}
		destPath := filepath.Join(dst, filepath.FromSlash(e.RelPath))
		if opts.DryRun {
			continue
		}
		if err := setAttrs(destPath, e.Info, false); err != nil {
			fmt.Fprintf(os.Stderr, "cpgo: attrs %s: %v\n", e.RelPath, err)
		}
	}

	if opts.Delete {
		if err := deleteExtraneous(dst, tree, opts); err != nil {
			return fmt.Errorf("deleting extraneous entries: %w", err)
		}
	}

	stopProgress()
	printFinalSummary(prog)

	if prog.Failed.Load() > 0 {
		return fmt.Errorf("%d entries failed to sync", prog.Failed.Load())
	}
	return nil
}

// syncFile copies srcPath to destPath if needed, verifying the copy by
// re-reading the destination and comparing checksums, then applies
// permissions/ownership/mtime. It is safe to re-run: interrupted copies
// leave a temp file that a later run simply overwrites.
func syncFile(srcPath, destPath string, srcInfo fs.FileInfo, opts Options, prog *Progress) error {
	if up, err := isUpToDate(srcPath, destPath, srcInfo); err != nil {
		return err
	} else if up {
		prog.DoneBytes.Add(srcInfo.Size())
		if opts.DryRun {
			return nil
		}
		return setAttrs(destPath, srcInfo, false)
	}

	if opts.DryRun {
		if opts.Verbose {
			fmt.Printf("would copy: %s\n", destPath)
		}
		prog.DoneBytes.Add(srcInfo.Size())
		return nil
	}

	attempts := opts.Retries + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 && opts.Verbose {
			fmt.Fprintf(os.Stderr, "cpgo: retrying %s (attempt %d/%d)\n", destPath, i+1, attempts)
		}
		var attemptBytes int64
		_, err := copyVerified(srcPath, destPath, srcInfo, func(n int64) {
			attemptBytes += n
			prog.DoneBytes.Add(n)
		})
		if err == nil {
			return setAttrs(destPath, srcInfo, false)
		}
		prog.DoneBytes.Add(-attemptBytes) // undo this attempt's partial progress before retrying
		lastErr = err
	}
	return fmt.Errorf("copy failed after %d attempts: %w", attempts, lastErr)
}

// isUpToDate decides whether destPath already holds a correct copy of
// srcPath. Size is checked first as a cheap exit (a size mismatch can never
// be up to date), but whenever sizes do match, both files are fully hashed
// and compared — this tool's whole point is to never trust metadata alone,
// so mtime is not treated as sufficient evidence on its own.
func isUpToDate(srcPath, destPath string, srcInfo fs.FileInfo) (bool, error) {
	dstInfo, err := os.Lstat(destPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if dstInfo.Mode()&fs.ModeSymlink != 0 || !dstInfo.Mode().IsRegular() {
		return false, nil // wrong type, must be replaced
	}
	if dstInfo.Size() != srcInfo.Size() {
		return false, nil
	}
	srcSum, err := hashFile(srcPath)
	if err != nil {
		return false, err
	}
	dstSum, err := hashFile(destPath)
	if err != nil {
		return false, err
	}
	return srcSum == dstSum, nil
}

// copyVerified copies srcPath into a temp file next to destPath, verifies the
// bytes actually landed on disk correctly by re-reading and re-hashing the
// temp file, and only then renames it into place. onBytes is called as data
// is read from the source, so callers can drive a live progress display; it
// may be nil. It returns the number of bytes copied on success.
func copyVerified(srcPath, destPath string, srcInfo fs.FileInfo, onBytes func(int64)) (int64, error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	tmpPath := destPath + ".cpgo.tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmpPath) // no-op once renamed away

	srcHash := sha256.New()
	var reader io.Reader = io.TeeReader(srcFile, srcHash)
	if onBytes != nil {
		reader = &countingReader{r: reader, onRead: onBytes}
	}
	n, err := io.Copy(tmpFile, reader)
	if err != nil {
		tmpFile.Close()
		return 0, err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return 0, fmt.Errorf("fsync: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return 0, err
	}

	// Guard against the source changing size while we were copying it.
	if fi, err := os.Stat(srcPath); err == nil && fi.Size() != srcInfo.Size() {
		return 0, fmt.Errorf("source changed size during copy")
	}

	dstSum, err := hashFile(tmpPath)
	if err != nil {
		return 0, err
	}
	if dstSum != fmt.Sprintf("%x", srcHash.Sum(nil)) {
		return 0, errors.New("checksum mismatch after copy: destination does not match source")
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return 0, err
	}
	return n, nil
}

// countingReader calls onRead with the number of bytes returned by each Read,
// letting callers observe progress as an io.Copy loop consumes r.
type countingReader struct {
	r      io.Reader
	onRead func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.onRead(int64(n))
	}
	return n, err
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// syncSymlink recreates a symlink if the destination doesn't already point
// at the same target.
func syncSymlink(srcPath, destPath string, srcInfo fs.FileInfo, opts Options) error {
	target, err := os.Readlink(srcPath)
	if err != nil {
		return err
	}
	if existing, err := os.Readlink(destPath); err == nil && existing == target {
		if opts.DryRun {
			return nil
		}
		return setAttrs(destPath, srcInfo, true)
	}
	if opts.DryRun {
		if opts.Verbose {
			fmt.Printf("would relink: %s -> %s\n", destPath, target)
		}
		return nil
	}
	if err := removeAny(destPath); err != nil {
		return err
	}
	if err := os.Symlink(target, destPath); err != nil {
		return err
	}
	return setAttrs(destPath, srcInfo, true)
}

// syncHardlink makes destPath a hardlink to primaryDestPath, skipping the
// work if it already is one.
func syncHardlink(primaryDestPath, destPath string) error {
	if same, err := sameInode(primaryDestPath, destPath); err == nil && same {
		return nil
	}
	if err := removeAny(destPath); err != nil {
		return err
	}
	return os.Link(primaryDestPath, destPath)
}

func sameInode(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	stA, ok1 := sa.Sys().(*syscall.Stat_t)
	stB, ok2 := sb.Sys().(*syscall.Stat_t)
	if !ok1 || !ok2 {
		return false, nil
	}
	return stA.Dev == stB.Dev && stA.Ino == stB.Ino, nil
}

// removeAny deletes whatever currently sits at path, if anything, so a new
// object of a possibly different type can take its place.
func removeAny(path string) error {
	err := os.RemoveAll(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// setAttrs applies permissions, ownership and modification time from info
// onto path. isSymlink selects the *l*-variants that don't follow links.
func setAttrs(path string, info fs.FileInfo, isSymlink bool) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	if isSymlink {
		// Ownership can be set on the link itself; mode and mtime of a
		// symlink are rarely meaningful and are not settable portably
		// without extra syscalls, so we leave them alone.
		if err := os.Lchown(path, int(st.Uid), int(st.Gid)); err != nil && !os.IsPermission(err) {
			return err
		}
		return nil
	}

	if err := os.Chmod(path, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Chown(path, int(st.Uid), int(st.Gid)); err != nil && !os.IsPermission(err) {
		return err
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	return nil
}

func printFinalSummary(p *Progress) {
	fmt.Printf("\ndone: %d/%d files, %s copied, %d failed\n",
		p.DoneFiles.Load(), p.TotalFiles, humanBytes(p.DoneBytes.Load()), p.Failed.Load())
}
