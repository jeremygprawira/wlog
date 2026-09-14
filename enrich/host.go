package enrich

import (
	"context"
	"os"

	"github.com/jeremygprawira/wlog"
)

// Host adds host.name, host.pid, and (when running on Kubernetes, per the downward
// API env vars) host.pod, host.namespace, host.node.
func Host(opts ...Option) wlog.Enricher {
	cfg := newConfig(opts)
	return wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
		fields := map[string]any{"pid": os.Getpid()}
		if name, err := os.Hostname(); err == nil {
			fields["name"] = name
		}
		if v := os.Getenv("POD_NAME"); v != "" {
			fields["pod"] = v
		}
		if v := os.Getenv("POD_NAMESPACE"); v != "" {
			fields["namespace"] = v
		}
		if v := os.Getenv("NODE_NAME"); v != "" {
			fields["node"] = v
		}
		mergeGroup(event, "host", fields, cfg.overwrite)
	})
}

// Deployment adds deploy.region (REGION env var), deploy.commit (GIT_COMMIT or
// COMMIT_SHA), and deploy.version (falling back to the event's own service.version).
func Deployment(opts ...Option) wlog.Enricher {
	cfg := newConfig(opts)
	return wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
		fields := map[string]any{}
		if v := os.Getenv("REGION"); v != "" {
			fields["region"] = v
		}
		if v := commitFromEnv(); v != "" {
			fields["commit"] = v
		}
		if v := serviceVersion(event); v != "" {
			fields["version"] = v
		}
		mergeGroup(event, "deploy", fields, cfg.overwrite)
	})
}

func commitFromEnv() string {
	if v := os.Getenv("GIT_COMMIT"); v != "" {
		return v
	}
	return os.Getenv("COMMIT_SHA")
}

func serviceVersion(event map[string]any) string {
	svc, ok := event["service"].(map[string]any)
	if !ok {
		return ""
	}
	v, _ := svc["version"].(string)
	return v
}
