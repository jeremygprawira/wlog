package golangci_test

import (
	"strings"
	"testing"

	"github.com/golangci/plugin-module-register/register"

	"github.com/jeremygprawira/wlog/cmd/wlog/golangci"
)

// TestGolangci_CLI13_PluginLoads proves the plugin registers itself under the analyzer's name,
// builds that analyzer, and asks golangci-lint for the syntax and type information the rules use.
func TestGolangci_CLI13_PluginLoads(t *testing.T) {
	factory, err := register.GetPlugin("wlogmap")
	if err != nil {
		t.Fatalf("the plugin is not registered: %v", err)
	}
	plugin, err := factory(nil)
	if err != nil {
		t.Fatalf("the factory: %v", err)
	}

	analyzers, err := plugin.BuildAnalyzers()
	if err != nil {
		t.Fatalf("BuildAnalyzers: %v", err)
	}
	if len(analyzers) != 1 || analyzers[0].Name != "wlogmap" {
		t.Fatalf("BuildAnalyzers returned %+v, want the wlogmap analyzer", analyzers)
	}
	if analyzers[0].Flags.Lookup("rules") == nil {
		t.Error("the analyzer arrives without its -rules flag")
	}

	mode := plugin.GetLoadMode()
	for _, want := range []string{register.LoadModeSyntax, register.LoadModeTypesInfo} {
		if !strings.Contains(mode, want) {
			t.Errorf("GetLoadMode = %q, want it to ask for %q", mode, want)
		}
	}

	// The exported constructor is the same path, so a caller that holds the package directly
	// gets a working plugin too.
	if _, err := golangci.New(nil); err != nil {
		t.Errorf("New: %v", err)
	}
}
