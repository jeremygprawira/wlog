// This file holds the drain: it ships every finished event to one subject, and it builds itself
// from the environment for setup.
package wlognats

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/nats-io/nats.go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/setup"
)

// Drain returns a wlog.Drain that publishes each event as one NATS message to one subject. The
// events batch and retry through pipeline.Wrap, and Close sends what is pending.
func Drain(p Publisher, subject string) wlog.Drain {
	return pipeline.Wrap(sender{p: p, subject: subject})
}

// sender publishes one batch of events as NATS messages.
type sender struct {
	p       Publisher
	subject string
}

// SendBatch publishes one message per event, and flushes the client buffer, so the batch is
// on the wire before the call returns. A publisher without a flush needs none.
func (s sender) SendBatch(ctx context.Context, events []map[string]any) error {
	for _, event := range events {
		body, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if err := s.p.PublishMsg(&nats.Msg{Subject: s.subject, Data: body}); err != nil {
			return err
		}
	}
	if flusher, ok := s.p.(interface {
		FlushWithContext(context.Context) error
	}); ok {
		return flusher.FlushWithContext(ctx)
	}
	return nil
}

// connDrain closes one connection when the Logger closes the drain, so a process that ends
// does not leave the connection open.
type connDrain struct {
	drain wlog.Drain
	conn  *nats.Conn
}

// Send passes one event to the wrapped drain.
func (d connDrain) Send(ctx context.Context, event map[string]any) { d.drain.Send(ctx, event) }

// Close stops the wrapped drain and closes the connection.
func (d connDrain) Close(ctx context.Context) error {
	if closer, ok := d.drain.(interface{ Close(context.Context) error }); ok {
		_ = closer.Close(ctx)
	}
	d.conn.Close()
	return nil
}

// Factory returns the setup factory of the NATS drain, so a deployment picks it with
// WLOG_DRAINS=nats. It reads WLOG_NATS_URL and WLOG_NATS_SUBJECT. Credentials and TLS need
// code.
func Factory() setup.Factory {
	return setup.Factory{
		Name: "nats",
		Vars: []setup.Var{
			{Name: "WLOG_NATS_URL", Required: true},
			{Name: "WLOG_NATS_SUBJECT", Required: true},
		},
		New: func(env setup.Env) (wlog.Drain, error) {
			url, _ := env.Lookup("WLOG_NATS_URL")
			subject, _ := env.Lookup("WLOG_NATS_SUBJECT")
			if url == "" || subject == "" {
				return nil, errors.New("wlognats: WLOG_NATS_URL and WLOG_NATS_SUBJECT are both required")
			}
			conn, err := nats.Connect(url)
			if err != nil {
				return nil, err
			}
			return connDrain{drain: Drain(conn, subject), conn: conn}, nil
		},
	}
}
