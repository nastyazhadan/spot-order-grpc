package health

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type Server struct {
	grpc_health_v1.UnimplementedHealthServer
}

func (s *Server) Check(
	ctx context.Context,
	request *grpc_health_v1.HealthCheckRequest,
) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

func (s *Server) Watch(
	request *grpc_health_v1.HealthCheckRequest,
	stream grpc_health_v1.Health_WatchServer,
) error {
	return stream.Send(&grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	})
}

func RegisterService(server *grpc.Server) {
	grpc_health_v1.RegisterHealthServer(server, &Server{})
}

func CheckHealth(ctx context.Context, connection *grpc.ClientConn) error {
	healthClient := grpc_health_v1.NewHealthClient(connection)

	response, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("grpc health check failed: %w", err)
	}

	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("service is not reachable: %s", response.GetStatus().String())
	}

	return nil
}
