module github.com/jeremygprawira/wlog/log/hclog

go 1.21

require (
	github.com/hashicorp/go-hclog v1.6.3
	github.com/jeremygprawira/wlog v0.7.0
)

require (
	github.com/fatih/color v1.13.0 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	golang.org/x/sys v0.0.0-20220503163025-988cb79eb6c6 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
