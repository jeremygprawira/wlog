module github.com/jeremygprawira/wlog/rpc/grpc

go 1.21

require (
	github.com/jeremygprawira/wlog v0.6.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240814211410-ddb44dafa142
	google.golang.org/grpc v1.67.3
)

require (
	golang.org/x/net v0.28.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.17.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
