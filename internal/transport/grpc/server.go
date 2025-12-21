package grpc

import (
	"context"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/internal/endpoint"
)

// Server is the gRPC server for TurboCIOrchestrator.
type Server struct {
	pb.UnimplementedTurboCIOrchestratorServer
	endpoints  endpoint.Endpoints
	grpcServer *grpc.Server
}

// NewServer creates a new gRPC server.
func NewServer(endpoints endpoint.Endpoints) *Server {
	s := &Server{
		endpoints: endpoints,
	}

	// Create gRPC server with interceptors
	s.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			LoggingInterceptor(),
			RecoveryInterceptor(),
		),
	)

	// Register the service
	pb.RegisterTurboCIOrchestratorServer(s.grpcServer, s)

	// Enable reflection for grpcurl and other tools
	reflection.Register(s.grpcServer)

	return s
}

// Serve starts the gRPC server on the given address.
func (s *Server) Serve(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	log.Printf("gRPC server listening on %s", addr)
	return s.grpcServer.Serve(lis)
}

// GracefulStop gracefully stops the server.
func (s *Server) GracefulStop() {
	s.grpcServer.GracefulStop()
}

// LoggingInterceptor returns a gRPC interceptor that logs requests.
func LoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		log.Printf("gRPC call: %s", info.FullMethod)
		resp, err := handler(ctx, req)
		if err != nil {
			log.Printf("gRPC error: %s: %v", info.FullMethod, err)
		}
		return resp, err
	}
}

// RecoveryInterceptor returns a gRPC interceptor that recovers from panics.
func RecoveryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("gRPC panic recovered: %s: %v", info.FullMethod, r)
				err = endpoint.MapErrorToStatus(nil) // Returns internal error
			}
		}()
		return handler(ctx, req)
	}
}
