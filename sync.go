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

// ErrChecksumMismatch means a freshly written copy was re-read and re-hashed
// but did not match the source. This is treated as confirmed file
// corruption (bad storage, bad RAM, etc.), distinct from ordinary I/O
// errors, and is what triggers Progress.triggerAbort.
var ErrChecksumMismatch = errors.New("checksum mismatch after copy: destination does not match source")

// Options controls how the sync runs.
type Options struct {
	DryRun  bool
	InPlace bool // write directly to the destination instead of temp file + rename
	Jobs    int  // concurrent file copies
	Retries int  // extra attempts after a checksum mismatch
	Verbose bool
}

// Progress is shared, atomically-updated state used to render the progress line.
type Progress struct {
	TotalBytes int64
	DoneBytes  atomic.Int64
	TotalFiles int64
	DoneFiles  atomic.Int64
	Failed     atomic.Int64

	// abortMu guards abortErr, which is set the moment confirmed file
	// corruption is detected. Once set, the rest of the sync stops as soon
	// as possible instead of continuing on to other files.
	abortMu  sync.Mutex
	abortErr error
}

// triggerAbort records err as the reason to stop, if nothing has triggered
// an abort yet. Safe to call from multiple goroutines.
func (p *Progress) triggerAbort(err error) {
	p.abortMu.Lock()
	defer p.abortMu.Unlock()
	if p.abortErr == nil {
		p.abortErr = err
	}
}

// aborted reports whether a confirmed corruption has already triggered a
// stop, and if so, the first reported reason.
func (p *Progress) aborted() (bool, error) {
	p.abortMu.Lock()
	defer p.abortMu.Unlock()
	return p.abortErr != nil, p.abortErr
}

// runSync recursively copies the tree rooted at src into dst according to
// opts, printing progress as it goes. Like `cp -R`, it never removes
// anything already present at dst.
func runSync(src, dst string, opts Options) error {
	if err := checkNotIntoSelf(src, dst); err != nil {
		return err
	}

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

	// Like `cp -R`, this creates at most the single leaf directory dst --
	// it's an error for dst's own parent directory to be missing, and
	// nothing above that is ever auto-created.
	if dstInfo, err := os.Stat(dst); err == nil {
		if !dstInfo.IsDir() {
			return fmt.Errorf("cannot overwrite non-directory %s with a directory", dst)
		}
	} else if os.IsNotExist(err) {
		if !opts.DryRun {
			if err := os.Mkdir(dst, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", dst, err)
			}
		}
	} else {
		return fmt.Errorf("stat %s: %w", dst, err)
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
		if aborted, _ := prog.aborted(); aborted {
			break // confirmed corruption elsewhere: stop scheduling new work
		}
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
				warnAndCountFailure(prog, err, "symlink %s", e.RelPath)
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
				handleFileSyncError(prog, e.RelPath, err)
			}
			prog.DoneFiles.Add(1)
		}()
	}
	wg.Wait()

	// Confirmed corruption stops everything immediately: skip hardlinks,
	// attribute fixup and deletion entirely rather than pressing on.
	if aborted, abortErr := prog.aborted(); aborted {
		stopProgress()
		printFinalSummary(prog)
		return fmt.Errorf("stopped: %w", abortErr)
	}

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
			warnAndCountFailure(prog, err, "hardlink %s", rel)
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
			logger.WithError(err).Warnf("attrs %s", e.RelPath)
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
			logger.Infof("retrying %s (attempt %d/%d)", destPath, i+1, attempts)
		}
		var attemptBytes int64
		_, err := copyVerified(srcPath, destPath, srcInfo, opts.InPlace, func(n int64) {
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

// handleFileSyncError records a per-file failure and decides what it means
// for the sync as a whole: confirmed corruption (a checksum mismatch) is
// logged as an error and stops the whole run via prog.triggerAbort; anything
// else is logged as a warning and the sync carries on to other files.
func handleFileSyncError(prog *Progress, relPath string, err error) {
	if errors.Is(err, ErrChecksumMismatch) {
		prog.Failed.Add(1)
		logger.WithError(err).Errorf("file corruption detected: %s", relPath)
		prog.triggerAbort(fmt.Errorf("file corruption detected: %s: %w", relPath, err))
		return
	}
	warnAndCountFailure(prog, err, "%s", relPath)
}

// warnAndCountFailure records a per-entry failure that doesn't abort the
// whole sync (unlike a confirmed checksum mismatch, handled directly in
// handleFileSyncError above) and logs it at warning level.
func warnAndCountFailure(prog *Progress, err error, format string, args ...interface{}) {
	prog.Failed.Add(1)
	logger.WithError(err).Warnf(format, args...)
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

// copyVerified copies srcPath to destPath, verifying the bytes actually
// landed on disk correctly by re-reading and re-hashing them. onBytes is
// called as data is read from the source, so callers can drive a live
// progress display; it may be nil. It returns the number of bytes copied on
// success.
//
// By default (inPlace=false) it writes into a temp file next to destPath and
// only renames it into place after verification succeeds, so a failed or
// interrupted copy never disturbs whatever was already at destPath. With
// inPlace=true it writes straight to destPath instead: this needs no spare
// disk space for a second copy of the file, but a copy that fails partway
// leaves destPath overwritten with bad or partial data rather than untouched
// -- the caller is trading away that safety net on purpose.
func copyVerified(srcPath, destPath string, srcInfo fs.FileInfo, inPlace bool, onBytes func(int64)) (int64, error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	dstFile, writePath, err := openCopyDest(destPath, inPlace)
	if err != nil {
		return 0, err
	}
	if !inPlace {
		defer os.Remove(writePath) // no-op once renamed away
	}

	srcHash := sha256.New()
	var reader io.Reader = io.TeeReader(srcFile, srcHash)
	if onBytes != nil {
		reader = &countingReader{r: reader, onRead: onBytes}
	}
	n, err := io.Copy(dstFile, reader)
	if err != nil {
		dstFile.Close()
		return 0, err
	}
	if err := dstFile.Sync(); err != nil {
		dstFile.Close()
		return 0, fmt.Errorf("fsync: %w", err)
	}
	if err := dstFile.Close(); err != nil {
		return 0, err
	}

	// Guard against the source changing size while we were copying it.
	if fi, err := os.Stat(srcPath); err == nil && fi.Size() != srcInfo.Size() {
		return 0, fmt.Errorf("source changed size during copy")
	}

	dstSum, err := hashFile(writePath)
	if err != nil {
		return 0, err
	}
	if dstSum != fmt.Sprintf("%x", srcHash.Sum(nil)) {
		return 0, ErrChecksumMismatch
	}

	if inPlace {
		return n, nil // already written straight to destPath, nothing to rename
	}
	if err := os.Rename(writePath, destPath); err != nil {
		return 0, err
	}
	return n, nil
}

// openCopyDest opens the file copy data should be written to, returning it
// along with the path actually opened. With inPlace=false (the default) this
// is a fresh temp file next to destPath, named with a random suffix (via
// os.CreateTemp) so two concurrent cpgo runs against the same destination
// don't collide -- mirroring how rclone names its own local .partial files.
// With inPlace=true it opens destPath directly, truncating it immediately.
func openCopyDest(destPath string, inPlace bool) (*os.File, string, error) {
	if inPlace {
		f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, "", err
		}
		return f, destPath, nil
	}
	f, err := os.CreateTemp(filepath.Dir(destPath), filepath.Base(destPath)+".*.partial")
	if err != nil {
		return nil, "", err
	}
	return f, f.Name(), nil
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
		if err := os.Lchown(path, int(st.Uid), int(st.Gid)); err != nil {
			if !os.IsPermission(err) {
				return err
			}
			warnOwnershipUnchanged(path, st.Uid, st.Gid, true)
		}
		return nil
	}

	if err := os.Chmod(path, info.Mode().Perm()); err != nil {
		if !os.IsPermission(err) {
			return err
		}
		warnPermissionUnchanged(path, info.Mode().Perm())
	}
	if err := os.Chown(path, int(st.Uid), int(st.Gid)); err != nil {
		if !os.IsPermission(err) {
			return err
		}
		warnOwnershipUnchanged(path, st.Uid, st.Gid, false)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	return nil
}

// warnOwnershipUnchanged logs a chown/lchown that failed for lack of
// permission, showing both the ownership that was wanted (from the source)
// and what the destination's ownership actually ended up being, so it's
// clear exactly how the copy diverges from the source.
func warnOwnershipUnchanged(path string, wantUid, wantGid uint32, isSymlink bool) {
	statFn := os.Stat
	if isSymlink {
		statFn = os.Lstat
	}
	warnAttrUnchanged(path, "ownership", fmt.Sprintf("uid=%d gid=%d", wantUid, wantGid), statFn,
		func(fi fs.FileInfo) (string, bool) {
			st, ok := fi.Sys().(*syscall.Stat_t)
			if !ok {
				return "", false
			}
			return fmt.Sprintf("uid=%d gid=%d", st.Uid, st.Gid), true
		})
}

// warnPermissionUnchanged logs a chmod that failed for lack of permission
// (i.e. the process owns neither the file nor is root), showing both the
// mode that was wanted (from the source) and what the destination's mode
// actually ended up being, so it's clear exactly how the copy diverges.
// Modes are printed in octal (e.g. 755), matching how permissions are
// normally written and how the user is likely to think of them.
func warnPermissionUnchanged(path string, wantMode fs.FileMode) {
	warnAttrUnchanged(path, "permissions", fmt.Sprintf("mode=%03o", wantMode.Perm()), os.Stat,
		func(fi fs.FileInfo) (string, bool) {
			return fmt.Sprintf("mode=%03o", fi.Mode().Perm()), true
		})
}

// warnAttrUnchanged is the shared shape behind warnOwnershipUnchanged and
// warnPermissionUnchanged: an attribute-setting syscall failed for lack of
// permission, so we stat the path afterward and log both what was wanted
// and (if it can be determined from Sys()) what the attribute was actually
// left as.
func warnAttrUnchanged(path, attr, wanted string, statFn func(string) (fs.FileInfo, error), actual func(fs.FileInfo) (string, bool)) {
	fi, err := statFn(path)
	if err != nil {
		logger.Warnf("%s not set on %s: wanted %s, and could not stat it afterward: %v",
			attr, path, wanted, err)
		return
	}
	if got, ok := actual(fi); ok {
		logger.Warnf("%s not set on %s: wanted %s, left as %s (permission denied)",
			attr, path, wanted, got)
		return
	}
	logger.Warnf("%s not set on %s: wanted %s (permission denied)", attr, path, wanted)
}

func printFinalSummary(p *Progress) {
	fmt.Printf("\ndone: %d/%d files, %s copied, %d failed\n",
		p.DoneFiles.Load(), p.TotalFiles, humanBytes(p.DoneBytes.Load()), p.Failed.Load())
}
