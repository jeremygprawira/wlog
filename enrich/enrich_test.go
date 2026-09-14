package enrich_test

import (
	"context"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog/enrich"
)

func TestHost_SetsNameAndPID(t *testing.T) {
	event := map[string]any{}
	enrich.Host().Enrich(context.Background(), event)

	host := event["host"].(map[string]any)
	if host["name"] == nil || host["name"] == "" {
		t.Error("host.name missing")
	}
	if host["pid"] != os.Getpid() {
		t.Errorf("host.pid = %v, want %d", host["pid"], os.Getpid())
	}
}

func TestHost_K8sEnvVars(t *testing.T) {
	t.Setenv("POD_NAME", "api-7f9c")
	t.Setenv("POD_NAMESPACE", "prod")
	t.Setenv("NODE_NAME", "node-3")

	event := map[string]any{}
	enrich.Host().Enrich(context.Background(), event)

	host := event["host"].(map[string]any)
	if host["pod"] != "api-7f9c" || host["namespace"] != "prod" || host["node"] != "node-3" {
		t.Errorf("host = %v", host)
	}
}

func TestHost_DoesNotOverwriteByDefault(t *testing.T) {
	event := map[string]any{"host": map[string]any{"name": "already-set"}}
	enrich.Host().Enrich(context.Background(), event)

	host := event["host"].(map[string]any)
	if host["name"] != "already-set" {
		t.Errorf("host.name = %v, want already-set (default Overwrite(false))", host["name"])
	}
	if host["pid"] == nil {
		t.Error("host.pid should still be added alongside the pre-set name")
	}
}

func TestHost_OverwriteTrue(t *testing.T) {
	event := map[string]any{"host": map[string]any{"name": "stale"}}
	enrich.Host(enrich.Overwrite(true)).Enrich(context.Background(), event)

	host := event["host"].(map[string]any)
	if host["name"] == "stale" {
		t.Error("Overwrite(true) did not replace an existing field")
	}
}

func TestDeployment_FromEnvAndService(t *testing.T) {
	t.Setenv("REGION", "ap-southeast-1")
	t.Setenv("GIT_COMMIT", "abc123")

	event := map[string]any{"service": map[string]any{"version": "1.4.0"}}
	enrich.Deployment().Enrich(context.Background(), event)

	deploy := event["deploy"].(map[string]any)
	if deploy["region"] != "ap-southeast-1" || deploy["commit"] != "abc123" || deploy["version"] != "1.4.0" {
		t.Errorf("deploy = %v", deploy)
	}
}
