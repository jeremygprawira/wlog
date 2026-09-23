module github.com/jeremygprawira/wlog/trace/otel

go 1.21

require (
	github.com/jeremygprawira/wlog v0.8.0
	go.opentelemetry.io/otel v1.20.0
	go.opentelemetry.io/otel/metric v1.20.0
	go.opentelemetry.io/otel/sdk v1.20.0
	go.opentelemetry.io/otel/sdk/metric v1.20.0
	go.opentelemetry.io/otel/trace v1.20.0
)

require (
	github.com/go-logr/logr v1.3.0 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	golang.org/x/sys v0.14.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
