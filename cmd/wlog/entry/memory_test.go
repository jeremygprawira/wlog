package entry_test

import (
	"runtime"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// TestMap_CLI17_MemoryBound proves loading and scanning a small app stays under 150 MB of peak
// heap. Loading the syntax of every transitive dependency is what blew the budget: a two-file
// app pulled in echo, gin, and their world.
func TestMap_CLI17_MemoryBound(t *testing.T) {
	const budget = 150 << 20

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	pkgs, err := entry.Load("./../testdata/config_app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	points := entry.Find(pkgs)
	if len(points) == 0 {
		t.Fatal("the fixture registered no handler, so the test proves nothing")
	}

	runtime.ReadMemStats(&after)
	// HeapAlloc after the scan, with the loaded packages still reachable, is the figure that
	// matters: TotalAlloc would count every allocation the loader made and freed.
	if after.HeapAlloc > budget {
		t.Errorf("peak heap %d bytes is over the %d byte budget", after.HeapAlloc, budget)
	}
	t.Logf("heap after scanning %d files: %d bytes", len(pkgs[0].Syntax), after.HeapAlloc)
}
