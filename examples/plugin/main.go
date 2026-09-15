// Command plugin shows a plugin that adds build metadata to every event. A plugin can
// implement any of Setup, Enricher, Keeper, or Drain; this one is an Enricher.
package main

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

// buildPlugin adds one build field to every event. wirePlugins registers it as an
// Enricher automatically, because it implements Enrich.
type buildPlugin struct{ version string }

// Name identifies the plugin in OnError reports.
func (buildPlugin) Name() string { return "build-plugin" }

// Enrich adds the plugin's version to the event.
func (p buildPlugin) Enrich(_ context.Context, event map[string]any) {
	event["build"] = map[string]any{"plugin_version": p.version}
}

func main() {
	log := wlog.New(
		wlog.WithPlugins(buildPlugin{version: "1.2.3"}),
		wlog.WithService("plugin-example", "0.0.1", "local"),
	)
	ctx := log.WithContext(context.Background())

	_, end := wlog.Start(ctx, "job.run")
	end()
}
