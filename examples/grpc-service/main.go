// Command grpc-service is a gRPC server with wlog around every call. It answers the
// standard health check, so the example uses real generated protobuf code.
package main

import (
	"context"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/jeremygprawira/wlog"
	wloggrpc "github.com/jeremygprawira/wlog/rpc/grpc"
)

// newServer builds one gRPC server with the wlog interceptors.
func newServer(logger *wlog.Logger) *grpc.Server {
	server := grpc.NewServer(wloggrpc.ServerOptions(logger)...)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	return server
}

func main() {
	logger := wlog.New(wlog.WithService("grpc-service", "0.0.1", "local"))
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", ":9090")
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(newServer(logger).Serve(listener))
}
