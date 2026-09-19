module github.com/jeremygprawira/wlog/log/zap

go 1.21

require go.uber.org/zap v1.28.0

require (
	github.com/stretchr/testify v1.12.1 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

require (
	github.com/jeremygprawira/wlog v0.6.0
	go.uber.org/multierr v1.10.0 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
