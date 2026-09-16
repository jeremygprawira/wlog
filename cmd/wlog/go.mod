module github.com/jeremygprawira/wlog/cmd/wlog

go 1.26.1

require (
	golang.org/x/tools v0.47.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/jeremygprawira/wlog v0.1.0
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..

replace github.com/jeremygprawira/wlog/errors/herr => ../../errors/herr

replace github.com/jeremygprawira/wlog/log/logrus => ../../log/logrus

replace github.com/jeremygprawira/wlog/log/zap => ../../log/zap

replace github.com/jeremygprawira/wlog/log/zerolog => ../../log/zerolog

replace github.com/jeremygprawira/wlog/middleware/echo => ../../middleware/echo

replace github.com/jeremygprawira/wlog/middleware/echo5 => ../../middleware/echo5

replace github.com/jeremygprawira/wlog/middleware/gin => ../../middleware/gin

replace github.com/jeremygprawira/wlog/trace/otel => ../../trace/otel
