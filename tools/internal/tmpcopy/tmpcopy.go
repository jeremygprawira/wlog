// Package tmpcopy copies the workspace into a temporary directory. A tool that upgrades a
// module then runs on the copy, so a killed run never leaves an upgraded go.mod in the
// tree and no other command in the workspace sees a half-upgraded module.
//
// The whole workspace is copied, not one module, because a module's tests read their
// siblings by relative path and some read the git history. A copy of one module alone
// cannot resolve either.
package tmpcopy

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Workspace copies the workspace at root into a new temporary directory and returns the
// path. The caller removes it.
func Workspace(root string) (string, error) {
	tmp, err := os.MkdirTemp("", "wlog-workspace-")
	if err != nil {
		return "", err
	}
	if err := copyTree(root, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

// copyTree copies every file under src into dst, the history included, because a test
// reads the committed baseline from it.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.Create(filepath.Join(dst, rel))
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}
