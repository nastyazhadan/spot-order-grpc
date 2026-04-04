package health

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type Server struct {
	grpc_health_v1.UnimplementedHealthServer

	mu     sync.Mutex
	status grpc_health_v1.HealthCheckResponse_ServingStatus
}

func NewServer() *Server {
	return &Server{
		status: grpc_health_v1.HealthCheckResponse_SERVING,
	}
}

func (s *Server) SetServing() {
	s.setStatus(grpc_health_v1.HealthCheckResponse_SERVING)
}

func (s *Server) SetNotServing() {
	s.setStatus(grpc_health_v1.HealthCheckResponse_NOT_SERVING)
}

func (s *Server) setStatus(next grpc_health_v1.HealthCheckResponse_ServingStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status = next
}

func (s *Server) currentStatus() grpc_health_v1.HealthCheckResponse_ServingStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.status
}

func (s *Server) Check(
	ctx context.Context,
	request *grpc_health_v1.HealthCheckRequest,
) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: s.currentStatus(),
	}, nil
}

func (s *Server) Watch(
	request *grpc_health_v1.HealthCheckRequest,
	stream grpc_health_v1.Health_WatchServer,
) error {
	return status.Error(codes.Unimplemented, "health watch is not implemented")
}

func RegisterService(server *grpc.Server, healthServer *Server) {
	grpc_health_v1.RegisterHealthServer(server, healthServer)
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
