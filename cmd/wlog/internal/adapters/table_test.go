package adapters_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/adapters"
)

// TestTable_WellFormed proves every row names a module, an import path under the wlog module,
// and a known kind, and that no name or import path repeats.
func TestTable_WellFormed(t *testing.T) {
	kinds := map[string]bool{
		"http": true, "rpc": true, "client": true, "store": true, "log": true,
		"errors": true, "flag": true, "queue": true, "job": true, "faas": true,
		"command": true, "ai": true, "trace": true, "metrics": true,
	}
	names := map[string]bool{}
	paths := map[string]bool{}
	for _, adapter := range adapters.Table {
		if adapter.Name == "" {
			t.Errorf("%+v has no name", adapter)
		}
		if names[adapter.Name] {
			t.Errorf("duplicate name %q", adapter.Name)
		}
		names[adapter.Name] = true
		if !strings.HasPrefix(adapter.Wlog, "github.com/jeremygprawira/wlog") {
			t.Errorf("%s has import path %q", adapter.Name, adapter.Wlog)
		}
		if paths[adapter.Wlog] {
			t.Errorf("duplicate import path %q", adapter.Wlog)
		}
		paths[adapter.Wlog] = true
		if !kinds[adapter.Kind] {
			t.Errorf("%s has kind %q", adapter.Name, adapter.Kind)
		}
		for _, lib := range adapter.Libs {
			if lib == "" || strings.HasPrefix(lib, "github.com/jeremygprawira/wlog") {
				t.Errorf("%s lists library %q", adapter.Name, lib)
			}
		}
	}
}

// TestTable_Sorted proves the table is sorted by name, so the plan output is stable.
func TestTable_Sorted(t *testing.T) {
	names := make([]string, 0, len(adapters.Table))
	for _, adapter := range adapters.Table {
		names = append(names, adapter.Name)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("the table is not sorted by name: %v", names)
	}
}

// TestTable_Lookup proves the two lookups find the chi adapter by its import path and by the
// chi library, because the same table serves detection and doctor.
func TestTable_Lookup(t *testing.T) {
	byPath, ok := adapters.ByWlog("github.com/jeremygprawira/wlog/middleware/chi")
	if !ok || byPath.Name != "http-chi" {
		t.Errorf("ByWlog(chi) = %+v, %v", byPath, ok)
	}
	found := false
	for _, adapter := range adapters.ByLib("github.com/go-chi/chi/v5") {
		if adapter.Name == "http-chi" {
			found = true
		}
	}
	if !found {
		t.Error("ByLib(chi/v5) does not name http-chi")
	}
	if lines := adapters.SetupLines(); lines["github.com/jeremygprawira/wlog/middleware/chi"] != "wlogchi.Setup(r)" {
		t.Errorf("SetupLines()[chi] = %q", lines["github.com/jeremygprawira/wlog/middleware/chi"])
	}
}
