package server

import (
	context "context"
	api "github.com/anshulsood11/loghouse/api/v1"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"time"
)

/*
CommitLog interface for Dependency Inversion so that the service is not tied
to a specific log implementation.
*/
type CommitLog interface {
	Append(*api.Record) (uint64, error)
	Read(uint64) (*api.Record, error)
}

type Authorizer interface {
	Authorize(subject, object, action string) error
}

type ServersFetcher interface {
	GetServers() ([]*api.Server, error)
}

type Config struct {
	CommitLog      CommitLog
	Authorizer     Authorizer
	ServersFetcher ServersFetcher
}

const (
	objectWildcard = "*"
	produceAction  = "produce"
	consumeAction  = "consume"
)

var _ api.LogServer = (*grpcServer)(nil)

type grpcServer struct {
	api.UnimplementedLogServer
	*Config
}

func NewGRPCServer(config *Config, grpcOpts ...grpc.ServerOption) (*grpc.Server, error) {
	logger := zap.L().Named("server")
	zapOpts := []grpc_zap.Option{
		grpc_zap.WithDurationField(
			func(duration time.Duration) zapcore.Field {
				return zap.Int64("grpc.time_ns", duration.Nanoseconds())
			},
		),
	}
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	err := view.Register(ocgrpc.DefaultServerViews...)
	if err != nil {
		return nil, err
	}
	grpcOpts = append(grpcOpts,
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			grpc_ctxtags.StreamServerInterceptor(),
			grpc_zap.StreamServerInterceptor(logger, zapOpts...),
			grpc_auth.StreamServerInterceptor(authenticate),
		)),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpc_ctxtags.UnaryServerInterceptor(),
			grpc_zap.UnaryServerInterceptor(logger, zapOpts...),
			grpc_auth.UnaryServerInterceptor(authenticate),
		)),
		grpc.StatsHandler(&ocgrpc.ServerHandler{}),
	)
	gsrvr := grpc.NewServer(grpcOpts...)
	srv := &grpcServer{
		Config: config,
	}
	api.RegisterLogServer(gsrvr, srv)
	return gsrvr, nil
}

func (s *grpcServer) Produce(ctx context.Context, req *api.ProduceRequest) (
	*api.ProduceResponse, error) {
	if err := s.Authorizer.Authorize(subject(ctx), objectWildcard, produceAction); err != nil {
		return nil, err
	}
	offset, err := s.CommitLog.Append(req.Record)
	if err != nil {
		return nil, err
	}
	return &api.ProduceResponse{Offset: offset}, nil
}

func (s *grpcServer) Consume(ctx context.Context, req *api.ConsumeRequest) (
	*api.ConsumeResponse, error) {
	if err := s.Authorizer.Authorize(subject(ctx), objectWildcard, consumeAction); err != nil {
		return nil, err
	}
	record, err := s.CommitLog.Read(req.Offset)
	if err != nil {
		return nil, err
	}
	return &api.ConsumeResponse{Record: record}, nil
}

func (s *grpcServer) GetServers(ctx context.Context, req *api.GetServersRequest) (
	*api.GetServersResponse, error) {
	servers, err := s.ServersFetcher.GetServers()
	if err != nil {
		return nil, err
	}
	return &api.GetServersResponse{Servers: servers}, nil
}

/*
ProduceStream implements a bidirectional streaming
RPC so the client can stream data into the server’s log and the server can tell
the client whether each request succeeded.
*/
func (s *grpcServer) ProduceStream(stream api.Log_ProduceStreamServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		res, err := s.Produce(stream.Context(), req)
		if err != nil {
			return err
		}
		if err = stream.Send(res); err != nil {
			return err
		}
	}
}

/*
ConsumeStream implements a server-side streaming RPC so the
client can tell the server where in the log to read records, and then the server
will stream every record that follows—even records that aren’t in the log yet!
When the server reaches the end of the log, the server will wait until someone
appends a record to the log and then continue streaming records to the client.
*/
func (s *grpcServer) ConsumeStream(req *api.ConsumeRequest, stream api.Log_ConsumeStreamServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		default:
			res, err := s.Consume(stream.Context(), req)
			switch err.(type) {
			case nil:
			case api.ErrOffsetOutOfRange:
				continue
			default:
				return err
			}
			if err = stream.Send(res); err != nil {
				return err
			}
			req.Offset++
		}
	}
}

// Authenticate is an interceptor/ middleware that reads the subject out of the client’s
// cert and writes it to the RPC’s context
func authenticate(ctx context.Context) (context.Context, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return ctx, status.New(codes.Unknown, "couldn't find peer info").Err()
	}
	if peer.AuthInfo == nil {
		return context.WithValue(ctx, subjectContextKey{}, ""), nil
	}
	tlsInfo := peer.AuthInfo.(credentials.TLSInfo)
	subject := tlsInfo.State.VerifiedChains[0][0].Subject.CommonName
	ctx = context.WithValue(ctx, subjectContextKey{}, subject)
	return ctx, nil
}

// subject returns the client’s cert’s subject, so that we can identify a client and check
// their access.
func subject(ctx context.Context) string {
	return ctx.Value(subjectContextKey{}).(string)
}

type subjectContextKey struct{}
