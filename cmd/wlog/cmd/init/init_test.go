package init_test

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// caseDef describes one framework fixture and the modules its generated tree needs.
type caseDef struct {
	framework string
	requires  []string
	replaces  map[string]string // module path -> path under the repo
}

// cases lists every framework init must support.
func cases() []caseDef {
	return []caseDef{
		{framework: "nethttp"},
		{framework: "mux", requires: []string{"github.com/gorilla/mux v1.8.1"}},
		{framework: "echo", requires: []string{"github.com/labstack/echo/v4 v4.15.4"},
			replaces: map[string]string{"github.com/jeremygprawira/wlog/middleware/echo": "middleware/echo"}},
		{framework: "echo5", requires: []string{"github.com/labstack/echo/v5 v5.3.1"},
			replaces: map[string]string{"github.com/jeremygprawira/wlog/middleware/echo5": "middleware/echo5"}},
		{framework: "gin", requires: []string{"github.com/gin-gonic/gin v1.12.0"},
			replaces: map[string]string{"github.com/jeremygprawira/wlog/middleware/gin": "middleware/gin"}},
	}
}

// repoRoot returns the repository root, three levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return root
}

// moduleFloor reads the go line of a go.mod file.
func moduleFloor(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	t.Fatalf("%s has no go line", path)
	return ""
}

// treeFor copies a fixture into a temp directory and writes its go.mod.
func treeFor(t *testing.T, tc caseDef, root string) string {
	t.Helper()
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "testdata", "inittrees", tc.framework, "main.go"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), source, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// The generated tree needs one floor: the floor of this module, because it
	// depends on the modules this module depends on.
	goMod := "module example.com/app\n\ngo " + moduleFloor(t, filepath.Join("..", "..", "go.mod")) +
		"\n\nrequire (\n\tgithub.com/jeremygprawira/wlog v0.0.0\n"
	for _, require := range tc.requires {
		goMod += "\t" + require + "\n"
	}
	for module := range tc.replaces {
		goMod += "\t" + module + " v0.0.0\n"
	}
	goMod += ")\n\nreplace github.com/jeremygprawira/wlog => " + root + "\n"
	for module, path := range tc.replaces {
		goMod += "replace " + module + " => " + filepath.Join(root, filepath.FromSlash(path)) + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

// TestInit_BuildsEveryFramework proves the generated tree compiles for all five.
func TestInit_BuildsEveryFramework(t *testing.T) {
	root := repoRoot(t)
	for _, tc := range cases() {
		t.Run(tc.framework, func(t *testing.T) {
			dir := treeFor(t, tc, root)
			if code := wloginit.Run([]string{"--dir", dir, "--framework", tc.framework}, io.Discard, io.Discard); code != 0 {
				t.Fatalf("init exit %d", code)
			}
			for _, name := range []string{"wlog.go", ".env.example"} {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Fatalf("%s missing: %v", name, err)
				}
			}
			build := exec.Command("go", "build", "./...")
			build.Dir = dir
			build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("generated tree does not build: %v\n%s", err, output)
			}
		})
	}
}

// TestInit_DryRunWritesNothing proves dry-run leaves the tree alone.
func TestInit_DryRunWritesNothing(t *testing.T) {
	root := repoRoot(t)
	dir := treeFor(t, caseDef{framework: "nethttp"}, root)

	var stdout bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir, "--dry-run"}, &stdout, io.Discard); code != 0 {
		t.Fatalf("dry run exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "wlog.go")); !os.IsNotExist(err) {
		t.Error("dry run wrote wlog.go")
	}
	if !bytes.Contains(stdout.Bytes(), []byte("wlog.go")) {
		t.Errorf("dry run printed no plan:\n%s", stdout.String())
	}
}

// TestInit_ExistingSetupFails proves an existing wlog.go is never overwritten.
func TestInit_ExistingSetupFails(t *testing.T) {
	root := repoRoot(t)
	dir := treeFor(t, caseDef{framework: "nethttp"}, root)
	if err := os.WriteFile(filepath.Join(dir, "wlog.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("seed wlog.go: %v", err)
	}
	if code := wloginit.Run([]string{"--dir", dir}, io.Discard, io.Discard); code != 1 {
		t.Errorf("exit %d, want 1 when wlog.go already exists", code)
	}
}

// TestInit_CLI4_NilMux proves a server started with a nil handler is wrapped around the default
// mux, so the generated app still gets one event per request, and that the tree builds.
func TestInit_CLI4_NilMux(t *testing.T) {
	root := repoRoot(t)
	dir := treeFor(t, caseDef{framework: "nethttp_nil"}, root)

	if code := wloginit.Run([]string{"--dir", dir, "--framework", "nethttp"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("init exit %d", code)
	}
	source := readFile(t, filepath.Join(dir, "main.go"))
	if !strings.Contains(source, "WrapHandler(http.DefaultServeMux)") {
		t.Errorf("a nil handler was not wrapped around the default mux:\n%s", source)
	}
	buildTree(t, dir)
}

// TestInit_CLI4_ServerLiteral proves an http.Server literal has its Handler field wrapped.
func TestInit_CLI4_ServerLiteral(t *testing.T) {
	root := repoRoot(t)
	dir := treeFor(t, caseDef{framework: "nethttp_server"}, root)

	if code := wloginit.Run([]string{"--dir", dir, "--framework", "nethttp"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("init exit %d", code)
	}
	source := readFile(t, filepath.Join(dir, "main.go"))
	if !strings.Contains(source, "Handler: WrapHandler(mux)") {
		t.Errorf("the server literal's Handler was not wrapped:\n%s", source)
	}
	buildTree(t, dir)
}

// TestInit_CLI15_AtomicWrites proves the run leaves no partial file, merges .env.example instead
// of replacing it, prints a unified diff on --dry-run, and refuses an unknown drain.
func TestInit_CLI15_AtomicWrites(t *testing.T) {
	root := repoRoot(t)
	dir := treeFor(t, caseDef{framework: "nethttp"}, root)

	// An existing .env.example with a line of its own, and one key the drain also wants.
	env := filepath.Join(dir, ".env.example")
	if err := os.WriteFile(env, []byte("# my own note\nWLOG_FILE_PATH=keep-me.ndjson\n"), 0o644); err != nil {
		t.Fatalf("seed env: %v", err)
	}
	mainBefore := readFile(t, filepath.Join(dir, "main.go"))

	// A dry run prints a unified diff and writes nothing.
	var stdout bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir, "--framework", "nethttp", "--drain", "file", "--dry-run"}, &stdout, io.Discard); code != 0 {
		t.Fatalf("dry run exit %d", code)
	}
	for _, marker := range []string{"---", "+++", "@@"} {
		if !strings.Contains(stdout.String(), marker) {
			t.Errorf("the dry run printed no unified diff marker %q:\n%s", marker, stdout.String())
		}
	}
	if got := readFile(t, filepath.Join(dir, "main.go")); got != mainBefore {
		t.Error("the dry run changed main.go")
	}
	if _, err := os.Stat(filepath.Join(dir, "wlog.go")); !os.IsNotExist(err) {
		t.Error("the dry run wrote wlog.go")
	}

	if code := wloginit.Run([]string{"--dir", dir, "--framework", "nethttp", "--drain", "file"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("init exit %d", code)
	}

	// The written env keeps the caller's lines and its value, and gains the drain's keys.
	merged := readFile(t, env)
	if !strings.Contains(merged, "# my own note") {
		t.Errorf("the merge dropped a caller's line:\n%s", merged)
	}
	if !strings.Contains(merged, "WLOG_FILE_PATH=keep-me.ndjson") {
		t.Errorf("the merge replaced a value the caller set:\n%s", merged)
	}

	// No leftover temp file, and a second run that fails at the plan stage changes nothing.
	assertNoTempFiles(t, dir)
	before := map[string]string{}
	for _, name := range []string{"main.go", ".env.example", "wlog.go"} {
		before[name] = readFile(t, filepath.Join(dir, name))
	}
	if code := wloginit.Run([]string{"--dir", dir, "--framework", "nethttp"}, io.Discard, io.Discard); code != 1 {
		t.Errorf("a second run exited %d, want 1: wlog.go already exists", code)
	}
	for name, want := range before {
		if got := readFile(t, filepath.Join(dir, name)); got != want {
			t.Errorf("a failed run changed %s", name)
		}
	}

	// An unknown drain is a usage error.
	if code := wloginit.Run([]string{"--dir", dir, "--drain", "nosuchdrain"}, io.Discard, io.Discard); code != 2 {
		t.Errorf("an unknown drain exited %d, want 2", code)
	}
}

// TestInit_CLI4_GeneratedAppServes proves the generated app builds, serves one request with a
// 200, and prints one event for it.
func TestInit_CLI4_GeneratedAppServes(t *testing.T) {
	root := repoRoot(t)
	dir := treeFor(t, caseDef{framework: "nethttp"}, root)
	if code := wloginit.Run([]string{"--dir", dir, "--framework", "nethttp"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("init exit %d", code)
	}
	buildTree(t, dir)

	port := freePort(t)
	addr := "127.0.0.1:" + port
	cmd := exec.Command(filepath.Join(dir, "app"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ADDR="+addr)
	events := &syncBuffer{}
	cmd.Stdout = events
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	status := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			status = resp.StatusCode
			_ = resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the app never answered)", status)
	}

	// The event is written when the request ends, so it may arrive a moment later.
	// The setup runs with env "local", so the console prints its tree rather than JSON;
	// either way the request produced one event, named by its operation.
	if !waitFor(t, 5*time.Second, func() bool { return strings.Contains(events.String(), "GET /") }) {
		t.Errorf("no event was logged:\n%s", events.String())
	}
}

// syncBuffer is a buffer one process writes and the test reads, with a lock between them.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends the process's output.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what the process has printed so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// readFile reads one file, and fails the test when it cannot.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// buildTree builds every package in the generated tree.
func buildTree(t *testing.T, dir string) {
	t.Helper()
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "app"), ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the generated tree does not build: %v\n%s", err, output)
	}
}

// assertNoTempFiles fails when the run left a partial file behind.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("the run left %s behind", entry.Name())
		}
	}
}

// freePort returns a port nothing is listening on.
func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	return port
}

// waitFor polls until the condition holds or the timeout passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}
