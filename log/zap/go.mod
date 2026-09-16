module github.com/jeremygprawira/wlog/log/zap

go 1.21

require go.uber.org/zap v1.28.0

require (
	github.com/jeremygprawira/wlog v0.1.0
	go.uber.org/multierr v1.10.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
