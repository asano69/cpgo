package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"syscall"
)

// Entry describes a single filesystem object discovered while scanning a tree.
// Info comes from Lstat, so symlinks are reported as symlinks rather than
// being followed.
type Entry struct {
	RelPath   string // path relative to the tree root, forward-slash separated
	AbsPath   string // absolute path on disk
	Info      fs.FileInfo
	IsDir     bool
	IsSymlink bool
	Dev       uint64
	Ino       uint64
}

// Tree is the result of scanning a directory tree.
type Tree struct {
	Root       string
	Entries    []*Entry        // parent directories always precede their children
	PathSet    map[string]bool // every RelPath present in the tree
	TotalBytes int64           // bytes to transfer: regular files, counted once per inode
	// HardlinkOf maps the RelPath of a secondary hardlink to the RelPath of the
	// first occurrence of that inode. Only the first occurrence is copied;
	// the rest are recreated with os.Link.
	HardlinkOf map[string]string
}

// scanTree walks root and returns every file, directory and symlink within it.
func scanTree(root string) (*Tree, error) {
	t := &Tree{
		Root:       root,
		PathSet:    make(map[string]bool),
		HardlinkOf: make(map[string]string),
	}
	// (dev, ino) -> RelPath of the first entry seen for that inode.
	seenInode := make(map[[2]uint64]string)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking %s: %w", path, err)
		}
		if path == root {
			return nil // don't record the root itself, only its contents
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		e := &Entry{
			RelPath:   rel,
			AbsPath:   path,
			Info:      info,
			IsDir:     d.IsDir(),
			IsSymlink: info.Mode()&fs.ModeSymlink != 0,
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			e.Dev = uint64(st.Dev)
			e.Ino = uint64(st.Ino)
		}

		t.PathSet[rel] = true
		t.Entries = append(t.Entries, e)

		if !e.IsDir && !e.IsSymlink && info.Mode().IsRegular() {
			key := [2]uint64{e.Dev, e.Ino}
			if first, ok := seenInode[key]; ok {
				t.HardlinkOf[rel] = first
			} else {
				seenInode[key] = rel
				t.TotalBytes += info.Size()
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}
