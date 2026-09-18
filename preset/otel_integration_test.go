//go:build integration

// This file runs the real OTel Collector with the shipped filelog configuration, so the
// claim that the configuration parses the otel preset is proven, not assumed.
package preset_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// collectorImage is the collector the integration stack pins, so the test and the stack
// read the same version.
const collectorImage = "otel/opentelemetry-collector-contrib:0.115.1"

// TestPreset_BET14_CollectorReadsFilelog proves that the shipped collector configuration
// turns each golden line into one log record.
func TestPreset_BET14_CollectorReadsFilelog(t *testing.T) {
	if !dockerReady() {
		t.Skip("docker is not running; run make integration")
	}
	yaml, err := filepath.Abs(filepath.Join("..", "integrations", "search", "collectors", "otel-filelog.yaml"))
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}

	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	out := filepath.Join(dir, "out")
	for _, sub := range []string{in, out} {
		if err := os.MkdirAll(sub, 0o750); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	goldens := goldenFiles(t)
	for _, name := range goldens {
		body, err := os.ReadFile(filepath.Join("testdata", "otel", name))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(in, name), body, 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	run := exec.Command("docker", "run", "-d", "--rm",
		"-v", in+":/in", "-v", out+":/out", "-v", yaml+":/etc/otelcol/config.yaml",
		collectorImage, "--config", "/etc/otelcol/config.yaml")
	id, err := run.Output()
	if err != nil {
		t.Fatalf("docker run: %v", err)
	}
	container := strings.TrimSpace(string(id))
	t.Cleanup(func() { _ = exec.Command("docker", "kill", container).Run() })

	// The collector reads the files at start and flushes on its timer, so the test waits
	// for the record count instead of assuming a fixed delay.
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		records, err := collectorRecords(filepath.Join(out, "logs.json"))
		if err == nil && len(records) == len(goldens) {
			return
		}
		time.Sleep(time.Second)
	}
	records, err := collectorRecords(filepath.Join(out, "logs.json"))
	t.Fatalf("collector records = %d (%v), want %d", len(records), err, len(goldens))
}

// dockerReady reports whether a docker daemon answers.
func dockerReady() bool {
	return exec.Command("docker", "info").Run() == nil
}

// goldenFiles returns the names of the OTel golden files.
func goldenFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", "otel"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// collectorRecords reads the log record bodies of the collector file exporter.
func collectorRecords(path string) ([]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file struct {
		ResourceLogs []struct {
			ScopeLogs []struct {
				LogRecords []struct {
					Body struct {
						StringValue string `json:"stringValue"`
					} `json:"body"`
				} `json:"logRecords"`
			} `json:"scopeLogs"`
		} `json:"resourceLogs"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		return nil, err
	}
	var bodies []string
	for _, resource := range file.ResourceLogs {
		for _, scope := range resource.ScopeLogs {
			for _, record := range scope.LogRecords {
				bodies = append(bodies, record.Body.StringValue)
			}
		}
	}
	return bodies, nil
}
