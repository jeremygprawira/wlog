package wlog_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// fieldMap is the optional view a Finisher uses to read every key.
type fieldMap interface {
	Fields() map[string]any
}

// writingFinisher writes into the map Fields returns.
type writingFinisher struct{}

func (writingFinisher) Name() string { return "writing-finisher" }

func (writingFinisher) OnFinish(_ context.Context, event wlog.Event) {
	view, ok := event.(fieldMap)
	if !ok {
		return
	}
	fields := view.Fields()
	fields["injected"] = "yes"
	if http, ok := fields["http"].(map[string]any); ok {
		http["status"] = 999
	}
}

// TestCore_O10_FieldsReturnsACopy proves a Finisher cannot change the event the drains get.
func TestCore_O10_FieldsReturnsACopy(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithPlugins(writingFinisher{}))
	ctx, end := wlog.Start(log.WithContext(context.Background()), "GET /orders")
	wlog.SetGroup(ctx, "http", "status", 200)
	end()

	event := rec.Last()
	if _, ok := event["injected"]; ok {
		t.Fatal("Fields() handed out the live map: injected reached the drain")
	}
	http, _ := event["http"].(map[string]any)
	if fmt.Sprint(http["status"]) != "200" {
		t.Fatalf("http.status = %v, want 200 after the Finisher wrote the Fields map", http["status"])
	}
}
