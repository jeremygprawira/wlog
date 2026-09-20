module github.com/jeremygprawira/wlog/rpc/connect

go 1.21

require (
	connectrpc.com/connect v1.18.1
	github.com/jeremygprawira/wlog v0.7.0
)

require google.golang.org/protobuf v1.34.2 // indirect

replace github.com/jeremygprawira/wlog => ../..
