package init_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I2_UseThatIsNotWlogStillWires proves a router.Use that is not wlog does not
// stop init, and a router that already calls wlog exits 0.
func TestInit_I2_UseThatIsNotWlogStillWires(t *testing.T) {
	t.Run("recoverer", func(t *testing.T) {
		dir := chiTree(t)
		main := filepath.Join(dir, "main.go")
		source := readFile(t, main)
		source = strings.Replace(source, "\"github.com/go-chi/chi/v5\"\n", "\"github.com/go-chi/chi/v5\"\n\t\"github.com/go-chi/chi/v5/middleware\"\n", 1)
		source = strings.Replace(source, "r := chi.NewRouter()\n", "r := chi.NewRouter()\n\tr.Use(middleware.Recoverer)\n", 1)
		if err := os.WriteFile(main, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		if code := wloginit.Run([]string{"--dir", dir, "--yes"}, io.Discard, &stderr); code != 0 {
			t.Fatalf("init exit %d\n%s", code, stderr.String())
		}
		got := readFile(t, main)
		if !strings.Contains(got, "LoggerMiddleware") {
			t.Fatalf("middleware was not installed:\n%s", got)
		}
	})
	t.Run("rest-api", func(t *testing.T) {
		dir := wholeRecipe(t, "rest-api")
		var stderr bytes.Buffer
		if code := wloginit.Run([]string{"--dir", dir, "--yes"}, io.Discard, &stderr); code != 0 {
			t.Fatalf("init exit %d\n%s", code, stderr.String())
		}
	})
}
