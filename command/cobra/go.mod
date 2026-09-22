module github.com/jeremygprawira/wlog/command/cobra

go 1.21

require (
	github.com/jeremygprawira/wlog v0.8.0
	github.com/spf13/cobra v1.2.0
	github.com/spf13/pflag v1.0.5
)

require github.com/inconshreveable/mousetrap v1.0.0 // indirect

replace github.com/jeremygprawira/wlog => ../..
