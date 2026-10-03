package init

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInit_I17_KeepsSymlinkAndMode proves a rewrite follows a symlink and keeps 0600.
func TestInit_I17_KeepsSymlinkAndMode(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.go")
	if err := os.WriteFile(real, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "main.go")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain.go")
	if err := os.WriteFile(plain, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(plain, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink became a regular file")
	}
	got, err := os.ReadFile(link)
	if err != nil || string(got) != "new" {
		t.Fatalf("link content = %q, %v", got, err)
	}
	for _, path := range []string{real, plain} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, st.Mode().Perm())
		}
	}
}
