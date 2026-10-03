package init_test

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	wlogdoctor "github.com/jeremygprawira/wlog/cmd/wlog/cmd/doctor"
	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// wholeRecipe copies one recipe app, including its tests, into a module that can build
// outside the workspace.
func wholeRecipe(t *testing.T, name string) string {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	source := filepath.Join(root, "examples", name)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", name, err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "examples", "go.mod"))
	if err != nil {
		t.Fatalf("read examples go.mod: %v", err)
	}
	examples := filepath.Join(root, "examples")
	var builder strings.Builder
	for _, line := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(line, "module ") {
			line = "module example.com/" + name
		}
		if module, target, ok := strings.Cut(line, " => "); ok && strings.HasPrefix(strings.TrimSpace(module), "replace ") {
			abs, err := filepath.Abs(filepath.Join(examples, strings.TrimSpace(target)))
			if err != nil {
				t.Fatalf("replace path: %v", err)
			}
			line = strings.TrimSpace(module) + " => " + abs
		}
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "examples", "go.sum"))
	if err != nil {
		t.Fatalf("read go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}
	return dir
}

// TestInit_I1_WholeRecipesBuildAndPassDoctor copies each recipe app, tests included, and
// runs init, go build, and wlog doctor.
func TestInit_I1_WholeRecipesBuildAndPassDoctor(t *testing.T) {
	recipes := []string{
		"llm-agent", "lambda", "rest-api", "mcp-server",
		"kafka-consumer", "cron-job", "cli-tool", "grpc-service",
	}
	for _, name := range recipes {
		t.Run(name, func(t *testing.T) {
			dir := wholeRecipe(t, name)
			var stderr bytes.Buffer
			if code := wloginit.Run([]string{"--dir", dir, "--yes"}, io.Discard, &stderr); code != 0 {
				t.Fatalf("init exit %d\n%s", code, stderr.String())
			}
			build := exec.Command("go", "build", "-o", os.DevNull, "./...")
			build.Dir = dir
			build.Env = append(os.Environ(), "GOWORK=off")
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, output)
			}
			if code := wlogdoctor.Run([]string{"--dir", dir}, io.Discard, io.Discard); code != 0 {
				t.Fatalf("wlog doctor exited %d", code)
			}
		})
	}
}
