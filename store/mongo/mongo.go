// Package wlogmongo adapts the work kit to the official MongoDB driver, so one command
// records one call on the open event.
//
// Read top to bottom: Monitor wraps the command monitor of the app. By default it sets
// only the Succeeded and Failed callbacks, so the driver copies no command body, and each
// call carries the duration the driver measured. WithCollection adds the Started
// callback, which reads the collection name and makes the driver copy every command.
//
// This is the whole setup:
//
//	opts.SetMonitor(wlogmongo.Monitor(opts.Monitor))
package wlogmongo

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/jeremygprawira/wlog"
)

// system names the database of every record.
const system = "mongodb"

// Option configures the monitor.
type Option func(*options)

// options holds the resolved settings of one monitor.
type options struct {
	collection bool
}

// WithCollection records the collection name in the target, as database.collection. It
// reads the command body, which makes the driver copy every command, so it is off by
// default.
func WithCollection() Option {
	return func(o *options) { o.collection = true }
}

// Monitor returns a command monitor that records one call per command and forwards every
// callback of next. Pass it to ClientOptions.SetMonitor.
func Monitor(next *event.CommandMonitor, opts ...Option) *event.CommandMonitor {
	cfg := options{}
	for _, opt := range opts {
		opt(&cfg)
	}
	m := &monitor{next: next, cfg: cfg}
	out := &event.CommandMonitor{
		Succeeded: m.succeeded,
		Failed:    m.failed,
	}
	// A Started callback makes the driver copy every command body, so the monitor
	// leaves it nil unless the app asked for the collection or already had one.
	if cfg.collection || (next != nil && next.Started != nil) {
		out.Started = m.started
	}
	return out
}

// monitor records commands and forwards to the monitor of the app.
type monitor struct {
	next        *event.CommandMonitor
	cfg         options
	mu          sync.Mutex
	collections map[startKey]string
}

// startKey identifies one command attempt, because the started and finished callbacks of
// one command share only the connection id and the request id.
type startKey struct {
	conn    string
	request int64
}

// started forwards the start of one command, and it keeps the collection name when the
// app asked for it.
func (m *monitor) started(ctx context.Context, evt *event.CommandStartedEvent) {
	defer ignorePanic()
	if m.next != nil && m.next.Started != nil {
		m.next.Started(ctx, evt)
	}
	if !m.cfg.collection {
		return
	}
	collection := firstString(evt.Command)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.collections == nil {
		m.collections = map[startKey]string{}
	}
	m.collections[startKey{conn: evt.ConnectionID, request: evt.RequestID}] = collection
}

// succeeded forwards one succeeded command and records it.
func (m *monitor) succeeded(ctx context.Context, evt *event.CommandSucceededEvent) {
	defer ignorePanic()
	if m.next != nil && m.next.Succeeded != nil {
		m.next.Succeeded(ctx, evt)
	}
	target := m.target(evt.ConnectionID, evt.RequestID, evt.DatabaseName)
	endCall(ctx, evt.CommandFinishedEvent, target, rowsOf(evt.Reply), nil)
}

// failed forwards one failed command and records it.
func (m *monitor) failed(ctx context.Context, evt *event.CommandFailedEvent) {
	defer ignorePanic()
	if m.next != nil && m.next.Failed != nil {
		m.next.Failed(ctx, evt)
	}
	target := m.target(evt.ConnectionID, evt.RequestID, evt.DatabaseName)
	endCall(ctx, evt.CommandFinishedEvent, target, 0, evt.Failure)
}

// target returns the target of one finished command, and it forgets the collection of
// that command.
func (m *monitor) target(conn string, request int64, database string) string {
	if !m.cfg.collection {
		return database
	}
	key := startKey{conn: conn, request: request}
	m.mu.Lock()
	collection := m.collections[key]
	delete(m.collections, key)
	m.mu.Unlock()
	if collection == "" {
		return database
	}
	return database + "." + collection
}

// endCall records one call for a finished command, with the duration the driver measured.
func endCall(ctx context.Context, evt event.CommandFinishedEvent, target string, rows int64, err error) {
	_, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "db", System: system, Operation: evt.CommandName, Target: target,
	})
	result := wlog.CallResult{Status: "ok", Duration: evt.Duration, Rows: rows}
	if err != nil {
		result.Status = ""
		result.Err = err
		result.ErrCode = errorCode(err)
	}
	end(result)
}

// rowsOf returns the number of documents a write answered, and zero for any other
// command.
func rowsOf(reply bson.Raw) int64 {
	if n, ok := reply.Lookup("n").AsInt64OK(); ok {
		return n
	}
	return 0
}

// firstString returns the string value of the first element of one command document,
// which is the collection name of a CRUD command.
func firstString(cmd bson.Raw) string {
	element, err := cmd.IndexErr(0)
	if err != nil {
		return ""
	}
	text, ok := element.Value().StringValueOK()
	if !ok {
		return ""
	}
	return text
}

// errorCode returns the code of one driver error, such as the server error name, and it
// never holds the error text.
func errorCode(err error) string {
	var command mongo.CommandError
	if errors.As(err, &command) {
		if command.Name != "" {
			return command.Name
		}
		if command.Code != 0 {
			return strconv.Itoa(int(command.Code))
		}
	}
	var write mongo.WriteException
	if errors.As(err, &write) {
		for _, we := range write.WriteErrors {
			if we.Code != 0 {
				return strconv.Itoa(we.Code)
			}
		}
	}
	return "error"
}

// ignorePanic keeps a wlog fault from reaching the app, because the driver calls the
// monitor with no recovery of its own.
func ignorePanic() {
	_ = recover()
}
