// Package tmpcopy copies one module into a temporary directory. A tool that upgrades a
// module then runs on the copy, so a killed run never leaves an upgraded go.mod in the
// tree and no other command in the workspace sees a half-upgraded module.
package tmpcopy

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Module copies the module at dir into a new temporary directory, and returns the path. The
// caller removes it. A relative replace path in the copy's go.mod becomes absolute, because
// the copy sits outside the tree.
func Module(dir string) (string, error) {
	tmp, err := os.MkdirTemp("", "wlog-module-")
	if err != nil {
		return "", err
	}
	if err := copyTree(dir, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	if err := absolutize(filepath.Join(tmp, "go.mod"), dir); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

// copyTree copies every file under src into dst. It skips a .git directory.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
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

// absolutize rewrites every relative replace path in a go.mod, so the copy resolves its
// siblings where they really live.
func absolutize(path, dir string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "replace ")
		fields := strings.Split(rest, "=>")
		if !ok || len(fields) != 2 {
			out.WriteString(line + "\n")
			continue
		}
		target := strings.TrimSpace(fields[1])
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		out.WriteString("replace " + strings.TrimSpace(fields[0]) + " => " + target + "\n")
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}
