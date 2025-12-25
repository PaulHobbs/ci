package grpc

import (
	"context"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"github.com/example/turboci-lite/internal/endpoint"
	"github.com/example/turboci-lite/internal/service"
)

// Server is the gRPC server for TurboCIOrchestrator.
type Server struct {
	pb.UnimplementedTurboCIOrchestratorServer
	endpoints       endpoint.Endpoints
	runnerService   *service.RunnerService
	callbackService *service.CallbackService
	grpcServer      *grpc.Server
}

// ServerOption is a functional option for configuring the Server.
type ServerOption func(*Server)

// WithRunnerService sets the runner service.
func WithRunnerService(svc *service.RunnerService) ServerOption {
	return func(s *Server) {
		s.runnerService = svc
	}
}

// WithCallbackService sets the callback service.
func WithCallbackService(svc *service.CallbackService) ServerOption {
	return func(s *Server) {
		s.callbackService = svc
	}
}

// NewServer creates a new gRPC server.
func NewServer(endpoints endpoint.Endpoints, opts ...ServerOption) *Server {
	s := &Server{
		endpoints: endpoints,
	}

	// Apply options
	for _, opt := range opts {
		opt(s)
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

// LoggingInterceptor returns a gRPC interceptor that logs requests and their duration.
func LoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()
		
		// Attempt to extract WorkPlanID for better logging
		wpID := extractWorkPlanID(req)
		
		resp, err := handler(ctx, req)
		
		duration := time.Since(start)
		if wpID != "" {
			log.Printf("gRPC call: %s [WP:%s] duration=%v", info.FullMethod, wpID, duration)
		} else {
			log.Printf("gRPC call: %s duration=%v", info.FullMethod, duration)
		}
		
		if err != nil {
			log.Printf("gRPC error: %s: %v", info.FullMethod, err)
		}
		return resp, err
	}
}

func extractWorkPlanID(req interface{}) string {
	type wpIDer interface {
		GetWorkPlanId() string
	}
	if w, ok := req.(wpIDer); ok {
		return w.GetWorkPlanId()
	}
	return ""
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
