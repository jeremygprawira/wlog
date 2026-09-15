package main

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestPlugin_EnrichesEveryEvent proves a registered plugin runs as an enricher without
// any extra option.
func TestPlugin_EnrichesEveryEvent(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithPlugins(buildPlugin{version: "1.2.3"}))
	ctx := log.WithContext(context.Background())

	_, end := wlog.Start(ctx, "job.run")
	end()

	build, _ := rec.Last()["build"].(map[string]any)
	if build["plugin_version"] != "1.2.3" {
		t.Errorf("build.plugin_version = %v, want 1.2.3", build["plugin_version"])
	}
}
