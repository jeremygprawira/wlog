module github.com/jeremygprawira/wlog/trace/otel

go 1.26.1

require go.opentelemetry.io/otel/trace v1.46.0

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/jeremygprawira/wlog v0.1.0
	go.opentelemetry.io/otel v1.46.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
