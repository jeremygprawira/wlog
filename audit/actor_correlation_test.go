package audit_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAudit_PAR26_AgentActor proves an agent actor keeps its model, its tools, and its
// prompt id, that the four actor types exist, and that a failed action can be recorded.
func TestAudit_PAR26_AgentActor(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "agent.run")

	audit.Do(ctx, audit.Record{
		Actor: audit.Actor{
			Type:     audit.ActorAgent,
			ID:       "agent-7",
			Model:    "claude-sonnet-4-5",
			Tools:    []string{"search", "refund"},
			PromptID: "prompt-3",
		},
		Action:  "invoice.refund",
		Target:  audit.Target{Type: "invoice", ID: "inv-1"},
		Outcome: audit.OutcomeFailure,
		Reason:  "policy",
	})
	end()

	record := recordOf(t, rec.Last())
	actor, _ := record["actor"].(map[string]any)
	if actor["type"] != audit.ActorAgent {
		t.Errorf("actor.type = %v, want %s", actor["type"], audit.ActorAgent)
	}
	if actor["model"] != "claude-sonnet-4-5" {
		t.Errorf("actor.model = %v, want the model", actor["model"])
	}
	tools, _ := actor["tools"].([]any)
	if len(tools) != 2 || tools[0] != "search" || tools[1] != "refund" {
		t.Errorf("actor.tools = %v, want the tool list", actor["tools"])
	}
	if actor["prompt_id"] != "prompt-3" {
		t.Errorf("actor.prompt_id = %v, want prompt-3", actor["prompt_id"])
	}
	if record["outcome"] != audit.OutcomeFailure {
		t.Errorf("outcome = %v, want %s", record["outcome"], audit.OutcomeFailure)
	}

	// The four actor types are the documented set.
	for name, want := range map[string]string{
		"AuditUser":    audit.ActorUser,
		"AuditService": audit.ActorService,
		"AuditSystem":  audit.ActorSystem,
		"AuditAgent":   audit.ActorAgent,
	} {
		if want != "user" && want != "service" && want != "system" && want != "agent" {
			t.Errorf("%s = %q, want one of user, service, system, agent", name, want)
		}
	}
	if !(audit.Actor{Type: audit.ActorAgent}).Valid() {
		t.Error("Actor.Valid rejected the agent type")
	}
	if (audit.Actor{Type: "robot"}).Valid() {
		t.Error("Actor.Valid accepted an unknown type")
	}
}

// TestAudit_PAR26_CorrelationDefaults proves a record takes correlation_id and
// causation_id from the event's trace group, and that a caller's own ids win.
func TestAudit_PAR26_CorrelationDefaults(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "order.create")
	wlog.SetGroup(ctx, "trace", "request_id", "req-1", "parent_event_id", "evt-9")

	audit.Do(ctx, testRecord())
	end()

	record := recordOf(t, rec.Last())
	if record["correlation_id"] != "req-1" {
		t.Errorf("correlation_id = %v, want the request id", record["correlation_id"])
	}
	if record["causation_id"] != "evt-9" {
		t.Errorf("causation_id = %v, want the parent event id", record["causation_id"])
	}

	// A caller that knows better keeps its own ids.
	own := testRecord()
	own.CorrelationID = "corr-7"
	own.CausationID = "cause-7"
	ctx, end = wlog.Start(log.WithContext(context.Background()), "order.create")
	wlog.SetGroup(ctx, "trace", "request_id", "req-1", "parent_event_id", "evt-9")
	audit.Do(ctx, own)
	end()

	record = recordOf(t, rec.Last())
	if record["correlation_id"] != "corr-7" || record["causation_id"] != "cause-7" {
		t.Errorf("ids = %v/%v, want the caller's own values", record["correlation_id"], record["causation_id"])
	}
}

// TestAudit_PAR26_IdempotencyKeyStable proves the default key is stable for one request
// id and differs for another, so a retried request is deduplicated and a new one is not.
func TestAudit_PAR26_IdempotencyKeyStable(t *testing.T) {
	keyFor := func(requestID string, r audit.Record) string {
		t.Helper()
		log, rec := wlogtest.New(t)
		ctx, end := wlog.Start(log.WithContext(context.Background()), "order.create")
		wlog.SetGroup(ctx, "trace", "request_id", requestID)
		audit.Do(ctx, r)
		end()
		key, _ := recordOf(t, rec.Last())["idempotency_key"].(string)
		return key
	}

	first := keyFor("req-1", testRecord())
	again := keyFor("req-1", testRecord())
	other := keyFor("req-2", testRecord())

	if len(first) != 32 {
		t.Errorf("idempotency_key = %q, want 32 hex characters", first)
	}
	if first != again {
		t.Errorf("the same request id gave %q and %q, want one stable key", first, again)
	}
	if first == other {
		t.Errorf("a different request id gave the same key %q", first)
	}

	// A caller's own key wins.
	own := testRecord()
	own.IdempotencyKey = "caller-key"
	if got := keyFor("req-1", own); got != "caller-key" {
		t.Errorf("idempotency_key = %q, want the caller's own key", got)
	}
}
