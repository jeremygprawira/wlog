// This file drives the Twirp hooks over the real generated service of the twirp module's
// example package, so the tests run the client and the server the library builds. It
// checks the server fields, the code of a handler error, the client call record, and an
// Error hook that runs with no prepared request.
package wlogtwirp_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/twitchtv/twirp"
	"github.com/twitchtv/twirp/example"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
	wlogtwirp "github.com/jeremygprawira/wlog/rpc/twirp"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestTwirp_C2_ServerFields proves that one unary call gives one rpc event with the fields
// of the work table, added to the request event by the server hooks.
func TestTwirp_C2_ServerFields(t *testing.T) {
	log, rec := wlogtest.New(t)
	server := newServer(t, log)

	_, err := newClient(server, nil).MakeHat(context.Background(), &example.Size{Inches: 10})
	if err != nil {
		t.Fatalf("MakeHat: %v", err)
	}

	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	got := rec.Last()
	if got["kind"] != "rpc" {
		t.Errorf("kind = %v, want rpc", got["kind"])
	}
	if got["level"] != "info" || got["outcome"] != "success" {
		t.Errorf("level/outcome = %v/%v, want info/success", got["level"], got["outcome"])
	}
	fields, _ := got["rpc"].(map[string]any)
	for key, want := range map[string]any{
		"system": "twirp", "package": "twitch.twirp.example", "service": "Haberdasher",
		"method": "MakeHat", "status_code": "OK",
	} {
		if fields[key] != want {
			t.Errorf("rpc.%s = %v, want %v", key, fields[key], want)
		}
	}
}

// TestTwirp_C2_ErrorCode proves that a Twirp code maps to the work table by name, never by
// the HTTP status, and that a client fault warns.
func TestTwirp_C2_ErrorCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	server := newServer(t, log)

	_, err := newClient(server, nil).MakeHat(context.Background(), &example.Size{Inches: 0})
	var twerr twirp.Error
	if !errors.As(err, &twerr) || twerr.Code() != twirp.InvalidArgument {
		t.Fatalf("error = %v, want an invalid_argument Twirp error", err)
	}

	got := rec.Last()
	if got["level"] != "warn" {
		t.Errorf("level = %v, want warn", got["level"])
	}
	fields, _ := got["rpc"].(map[string]any)
	if fields["status_code"] != "InvalidArgument" {
		t.Errorf("rpc.status_code = %v, want InvalidArgument", fields["status_code"])
	}
}

// TestTwirp_C2_ClientCallRecord proves that one client call gives one call record with the
// fields of the calls shape.
func TestTwirp_C2_ClientCallRecord(t *testing.T) {
	log, rec := wlogtest.New(t)
	server := newServerPlain(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	_, err := newClient(server, wlogtwirp.ClientHooks()).MakeHat(ctx, &example.Size{Inches: 10})
	end()
	if err != nil {
		t.Fatalf("MakeHat: %v", err)
	}

	record := lastCall(t, rec)
	for key, want := range map[string]any{
		"kind": "rpc", "system": "twirp", "status": "OK",
		"operation": "twitch.twirp.example.Haberdasher/MakeHat",
		"target":    strings.TrimPrefix(server.URL, "http://"),
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	if _, ok := record["duration_ms"]; !ok {
		t.Error("calls[0].duration_ms is missing")
	}
}

// TestTwirp_C2_ClientError proves that a failed client call records the code and the
// error.
func TestTwirp_C2_ClientError(t *testing.T) {
	log, rec := wlogtest.New(t)
	server := newServerPlain(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	_, err := newClient(server, wlogtwirp.ClientHooks()).MakeHat(ctx, &example.Size{Inches: 0})
	end()
	if err == nil {
		t.Fatal("MakeHat returned no error")
	}

	record := lastCall(t, rec)
	if record["status"] != "InvalidArgument" {
		t.Errorf("calls[0].status = %v, want InvalidArgument", record["status"])
	}
	callError, _ := record["error"].(map[string]any)
	if callError["code"] != "InvalidArgument" {
		t.Errorf("calls[0].error.code = %v, want InvalidArgument", callError["code"])
	}
}

// TestTwirp_PAR_ErrorHookAlone proves that the Error hook accepts a context with no
// prepared request before it.
func TestTwirp_PAR_ErrorHookAlone(t *testing.T) {
	hooks := wlogtwirp.ClientHooks()
	if hooks.Error == nil {
		t.Fatal("ClientHooks returned no Error hook")
	}
	hooks.Error(context.Background(), twirp.InternalError("the call failed"))
}

// newServer starts a Twirp server with the wlog hooks, wrapped in the net/http middleware
// that starts the request event.
func newServer(t *testing.T, log *wlog.Logger) *httptest.Server {
	t.Helper()
	handler := wlogstd.Middleware(log)(example.NewHaberdasherServer(haberdasher{}, twirp.WithServerHooks(wlogtwirp.ServerHooks())))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// newServerPlain starts a Twirp server with no wlog hook, so a client test reads only the
// call record.
func newServerPlain(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(example.NewHaberdasherServer(haberdasher{}))
	t.Cleanup(server.Close)
	return server
}

// newClient returns a Twirp client of one test server, with optional hooks.
func newClient(server *httptest.Server, hooks *twirp.ClientHooks) example.Haberdasher {
	opts := []twirp.ClientOption{}
	if hooks != nil {
		opts = append(opts, twirp.WithClientHooks(hooks))
	}
	return example.NewHaberdasherProtobufClient(server.URL, server.Client(), opts...)
}

// haberdasher answers the example calls. A size of zero gives an invalid_argument error.
type haberdasher struct{}

// MakeHat answers one call with a hat of the asked size.
func (haberdasher) MakeHat(_ context.Context, size *example.Size) (*example.Hat, error) {
	if size.Inches <= 0 {
		return nil, twirp.InvalidArgumentError("inches", "must be positive")
	}
	return &example.Hat{Size: size.Inches, Color: "blue", Name: "bowler"}, nil
}

// lastCall returns the only call record of the last event.
func lastCall(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	return record
}
