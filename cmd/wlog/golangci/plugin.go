// Package golangci registers the wlog map analyzer as a golangci-lint v2 module plugin, so a
// team can run the rules from golangci-lint beside its other linters instead of from go vet.
//
// Build the custom binary with a .custom-gcl.yml file:
//
//	version: v2.4.0
//	plugins:
//	  - module: github.com/jeremygprawira/wlog/cmd/wlog
//	    import: github.com/jeremygprawira/wlog/cmd/wlog/golangci
//	    path: .
//
// Then `make custom-gcl` writes custom-gcl, and `custom-gcl run` runs the analyzer. The plugin
// needs the module's own path, because golangci-lint builds it from source.
package golangci

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"

	"github.com/jeremygprawira/wlog/cmd/wlog/analyzer"
)

// pluginName is the id the plugin answers to, which is also the analyzer's name.
const pluginName = "wlogmap"

// init registers the plugin under its name. golangci-lint calls the factory it finds here.
func init() {
	register.Plugin(pluginName, New)
}

// Plugin holds one configured instance of the analyzer set.
type Plugin struct{}

// New builds the plugin. The settings are empty for now: the analyzer's own flags carry what a
// run needs, and golangci-lint passes them through.
func New(_ any) (register.LinterPlugin, error) {
	return &Plugin{}, nil
}

// BuildAnalyzers returns the analyzers this plugin contributes.
func (p *Plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{analyzer.Analyzer}, nil
}

// GetLoadMode asks golangci-lint for syntax and type information, which the rules need: they
// follow a handler to its declaration and read the types of the calls it makes.
func (p *Plugin) GetLoadMode() string {
	return register.LoadModeSyntax + "," + register.LoadModeTypesInfo
}
