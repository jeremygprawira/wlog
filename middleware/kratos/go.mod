module github.com/jeremygprawira/wlog/middleware/kratos

go 1.21

require (
	github.com/go-kratos/kratos/v2 v2.8.4
	github.com/jeremygprawira/wlog v0.8.0
	github.com/jeremygprawira/wlog/rpc/grpc v0.8.0
	google.golang.org/grpc v1.67.3
)

require (
	github.com/go-kratos/aegis v0.2.0 // indirect
	github.com/go-playground/form/v4 v4.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	golang.org/x/net v0.28.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.17.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240814211410-ddb44dafa142 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240814211410-ddb44dafa142 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/jeremygprawira/wlog => ../..
	github.com/jeremygprawira/wlog/rpc/grpc => ../../rpc/grpc
)
