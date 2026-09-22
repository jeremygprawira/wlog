module github.com/jeremygprawira/wlog/queue/watermill

go 1.21

require (
	github.com/ThreeDotsLabs/watermill v1.4.7
	github.com/jeremygprawira/wlog v0.8.0
)

require (
	github.com/cenkalti/backoff/v3 v3.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/lithammer/shortuuid/v3 v3.0.7 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sony/gobreaker v1.0.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
