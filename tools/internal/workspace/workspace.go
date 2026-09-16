// Package workspace reads the go.work workspace and maps a git diff onto modules.
//
// The flow is two passes. Modules reads go.work, then reads the go.mod of every
// module for its path, its Go floor, and its requires. The second pass fills
// Dependents from those requires, so every module knows who needs it. Affected
// runs git diff for a base ref, maps each changed file to the module that owns
// it, and then adds every module that depends on a touched module, directly or
// through a chain.
package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Module describes one module of the workspace.
//
// Dir is the path as go.work writes it, so the root module is ".". Floor is the
// go directive of the module's go.mod. Requires and Dependents hold module
// paths, and they are never nil, so JSON shows an empty list.
type Module struct {
	Path       string   `json:"path"`
	Dir        string   `json:"dir"`
	Floor      string   `json:"floor"`
	Requires   []string `json:"requires"`
	Dependents []string `json:"dependents"`
}

// FindRoot walks up from dir until it finds the directory that holds go.work,
// and returns that directory. It fails when no directory holds go.work, so a
// command never reads a half-workspace above the repository.
func FindRoot(dir string) (string, error) {
	for cur := filepath.Clean(dir); ; {
		if _, err := os.Stat(filepath.Join(cur, "go.work")); err == nil {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no go.work at or above %s", dir)
		}
		cur = parent
	}
}

// Modules reads go.work in root and returns one entry per module, sorted by dir.
//
// A module entry with a missing or unreadable go.mod is an error, because a
// half-read workspace would hide a module from every later command.
func Modules(root string) ([]Module, error) {
	dirs, err := useDirs(filepath.Join(root, "go.work"))
	if err != nil {
		return nil, err
	}

	mods := make([]Module, 0, len(dirs))
	for _, dir := range dirs {
		path, floor, requires, err := readGoMod(filepath.Join(root, dir, "go.mod"))
		if err != nil {
			return nil, err
		}
		mods = append(mods, Module{
			Path:       path,
			Dir:        dir,
			Floor:      floor,
			Requires:   requires,
			Dependents: []string{},
		})
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Dir < mods[j].Dir })

	for i := range mods {
		for _, req := range mods[i].Requires {
			if j := indexOfPath(mods, req); j >= 0 {
				mods[j].Dependents = append(mods[j].Dependents, mods[i].Path)
			}
		}
	}
	for i := range mods {
		sort.Strings(mods[i].Dependents)
	}
	return mods, nil
}

// Affected returns the dirs of the modules that the diff from base touches,
// plus the dirs of every module that depends on those modules.
//
// It runs git diff --name-only base inside root, so the result covers committed
// and uncommitted changes. The result is sorted, and it is empty when the diff
// touches no module file.
func Affected(root, base string) ([]string, error) {
	files, err := changedFiles(root, base)
	if err != nil {
		return nil, err
	}
	mods, err := Modules(root)
	if err != nil {
		return nil, err
	}

	touched := map[string]bool{}
	for _, file := range files {
		if i := ownerIndex(mods, file); i >= 0 {
			touched[mods[i].Dir] = true
		}
	}

	// Walk the requires until no new module joins, so a chain of dependents
	// reports its top as well.
	for grew := true; grew; {
		grew = false
		for i := range mods {
			if touched[mods[i].Dir] {
				continue
			}
			for _, req := range mods[i].Requires {
				if j := indexOfPath(mods, req); j >= 0 && touched[mods[j].Dir] {
					touched[mods[i].Dir] = true
					grew = true
					break
				}
			}
		}
	}

	out := make([]string, 0, len(touched))
	for dir := range touched {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out, nil
}

// changedFiles runs git diff --name-only for base inside root.
func changedFiles(root, base string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "diff", "--name-only", base)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s: %w", base, err)
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// ownerIndex returns the index of the module that owns file, or -1.
//
// The longest matching dir wins, so a file under ./middleware/gin belongs to
// that module and not to the root module.
func ownerIndex(mods []Module, file string) int {
	best, bestLen := -1, -1
	for i, m := range mods {
		dir := strings.TrimPrefix(filepath.ToSlash(m.Dir), "./")
		if dir == "." {
			dir = ""
		}
		if dir != "" && file != dir && !strings.HasPrefix(file, dir+"/") {
			continue
		}
		if len(dir) > bestLen {
			best, bestLen = i, len(dir)
		}
	}
	return best
}

// indexOfPath returns the index of the module with this module path, or -1.
func indexOfPath(mods []Module, path string) int {
	for i, m := range mods {
		if m.Path == path {
			return i
		}
	}
	return -1
}

// useDirs returns the use entries of a go.work file, in file order.
//
// It reads the plain `use x` form and the `use ( ... )` block form. A trailing
// // comment is dropped, and a quoted path is unquoted.
func useDirs(file string) ([]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var dirs []string
	inBlock := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(comment(raw))
		switch {
		case line == "":
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			dirs = append(dirs, unquote(line))
		case line == "use (":
			inBlock = true
		case strings.HasPrefix(line, "use "):
			dirs = append(dirs, unquote(strings.TrimSpace(line[len("use "):])))
		}
	}
	return dirs, nil
}

// comment drops a trailing // comment from a go.work line.
func comment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// unquote removes the quotes around a go.work path when they are present.
func unquote(s string) string {
	if strings.HasPrefix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// readGoMod returns the module path, the Go floor, and the required module
// paths of one go.mod file.
//
// It reads the single and the block form of require. An indirect require counts
// as a dependency, because the dependent module still rebuilds with it.
func readGoMod(file string) (path, floor string, requires []string, err error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", "", nil, err
	}

	requires = []string{}
	inBlock := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(comment(raw))
		switch {
		case line == "":
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			if f := strings.Fields(line); len(f) > 0 {
				requires = append(requires, f[0])
			}
		case line == "require (":
			inBlock = true
		case strings.HasPrefix(line, "require "):
			if f := strings.Fields(line[len("require "):]); len(f) > 0 {
				requires = append(requires, f[0])
			}
		case strings.HasPrefix(line, "module "):
			path = strings.TrimSpace(line[len("module "):])
		case strings.HasPrefix(line, "go "):
			floor = strings.TrimSpace(line[len("go "):])
		}
	}
	if path == "" {
		return "", "", nil, fmt.Errorf("%s: no module directive", file)
	}
	return path, floor, requires, nil
}
