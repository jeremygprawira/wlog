module github.com/jeremygprawira/wlog/rpc/twirp

go 1.21

require (
	github.com/jeremygprawira/wlog v0.7.0
	github.com/twitchtv/twirp v8.1.0+incompatible
)

require (
	github.com/pkg/errors v0.9.1 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/jeremygprawira/wlog => ../..
