package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// deleteExtraneous removes anything under dst that has no counterpart in
// tree (the scanned source). Deletion runs deepest-first so directories
// empty out before the directory itself is removed.
func deleteExtraneous(dst string, tree *Tree, opts Options) error {
	var destRelPaths []string
	err := filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dst {
			return nil
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil {
			return err
		}
		destRelPaths = append(destRelPaths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing at dst yet, nothing to delete
		}
		return err
	}

	// Deepest paths first, so files are removed before their parent directory.
	sort.Slice(destRelPaths, func(i, j int) bool { return destRelPaths[i] > destRelPaths[j] })

	for _, rel := range destRelPaths {
		if tree.PathSet[rel] {
			continue
		}
		fullPath := filepath.Join(dst, filepath.FromSlash(rel))
		if opts.DryRun || opts.Verbose {
			fmt.Printf("delete: %s\n", fullPath)
		}
		if opts.DryRun {
			continue
		}
		if err := os.RemoveAll(fullPath); err != nil {
			return fmt.Errorf("remove %s: %w", fullPath, err)
		}
	}
	return nil
}
