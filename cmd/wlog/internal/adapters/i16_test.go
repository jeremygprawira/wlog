package adapters_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/adapters"
)

// TestAdapters_I16_KafkaAliasAndCloudWatch proves the kafka-go alias matches the package
// and CloudWatch has a row.
func TestAdapters_I16_KafkaAliasAndCloudWatch(t *testing.T) {
	var kafka string
	var cloud bool
	for _, adapter := range adapters.Table {
		if strings.Contains(adapter.Setup, "wlogkafka.") {
			t.Fatalf("stale alias in %s: %s", adapter.Name, adapter.Setup)
		}
		if adapter.Name == "queue-kafkago" {
			kafka = adapter.Setup
		}
		if adapter.Wlog == "github.com/jeremygprawira/wlog/drain/cloudwatch" {
			cloud = true
		}
	}
	if !strings.Contains(kafka, "wlogkafkago.") {
		t.Fatalf("kafka setup = %q", kafka)
	}
	if !cloud {
		t.Fatal("drain/cloudwatch has no row")
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "queue", "kafkago", "doc.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(doc), "Package wlogkafka ") {
		t.Fatal("doc.go still names the package wlogkafka")
	}
}
