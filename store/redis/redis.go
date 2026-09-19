// Package wlogredis adapts the work kit to go-redis, so one command or pipeline records
// one call on the open event, with no key and no argument.
//
// Read top to bottom: Hook wraps the three go-redis hooks. ProcessHook records one
// command, and a miss of a key is not an error. ProcessPipelineHook records one call for
// a whole pipeline, with the command count in rows. DialHook forwards the connection
// dial.
//
// This is the whole setup:
//
//	rdb := redis.NewClient(&redis.Options{Addr: addr})
//	rdb.AddHook(wlogredis.Hook())
package wlogredis

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/jeremygprawira/wlog"
)

// Hook returns the go-redis hook that records one call per command and per pipeline.
// Install it with rdb.AddHook before the first use of the client.
func Hook() redis.Hook { return hook{} }

// hook records commands and pipelines.
type hook struct{}

// DialHook forwards one connection dial.
func (hook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

// ProcessHook records one command. A miss of a key records the status miss and no error,
// because a missing key is an answer and not a failure.
func (hook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		ctx, end := wlog.StartCall(ctx, wlog.Call{
			Kind: "cache", System: "redis", Operation: cmd.Name(),
		})
		err := next(ctx, cmd)
		switch {
		case errors.Is(err, redis.Nil):
			end(wlog.CallResult{Status: "miss"})
		case err != nil:
			end(wlog.CallResult{Err: err, ErrCode: errorCode(err)})
		default:
			end(wlog.CallResult{Status: "ok"})
		}
		return err
	}
}

// ProcessPipelineHook records one call for a whole pipeline, with its command count in
// rows.
func (hook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		ctx, end := wlog.StartCall(ctx, wlog.Call{
			Kind: "cache", System: "redis", Operation: "pipeline",
		})
		err := next(ctx, cmds)
		switch {
		case errors.Is(err, redis.Nil):
			end(wlog.CallResult{Status: "miss", Rows: int64(len(cmds))})
		case err != nil:
			end(wlog.CallResult{Rows: int64(len(cmds)), Err: err, ErrCode: errorCode(err)})
		default:
			end(wlog.CallResult{Status: "ok", Rows: int64(len(cmds))})
		}
		return err
	}
}

// errorCode returns the prefix of one Redis error, such as ERR or WRONGTYPE, and it never
// holds the error text.
func errorCode(err error) string {
	var redisErr redis.Error
	if errors.As(err, &redisErr) {
		if fields := strings.Fields(redisErr.Error()); len(fields) > 0 {
			return fields[0]
		}
	}
	return "error"
}
