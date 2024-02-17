package server

import (
	context "context"
	log_v1 "github.com/anshulsood11/loghouse/api/v1"
	"google.golang.org/grpc"
)

/*
CommitLog interface for Dependency Inversion so that the service is not tied
to a specific log implementation.
*/
type CommitLog interface {
	Append(*log_v1.Record) (uint64, error)
	Read(uint64) (*log_v1.Record, error)
}
type Config struct {
	CommitLog CommitLog
}

var _ log_v1.LogServer = (*grpcServer)(nil)

type grpcServer struct {
	log_v1.UnimplementedLogServer
	*Config
}

func NewGRPCServer(config *Config) (*grpc.Server, error) {
	gsrvr := grpc.NewServer()
	srv := &grpcServer{
		Config: config,
	}
	log_v1.RegisterLogServer(gsrvr, srv)
	return gsrvr, nil
}

func (s *grpcServer) Produce(ctx context.Context, req *log_v1.ProduceRequest) (
	*log_v1.ProduceResponse, error) {
	offset, err := s.CommitLog.Append(req.Record)
	if err != nil {
		return nil, err
	}
	return &log_v1.ProduceResponse{Offset: offset}, nil
}

func (s *grpcServer) Consume(ctx context.Context, req *log_v1.ConsumeRequest) (
	*log_v1.ConsumeResponse, error) {
	record, err := s.CommitLog.Read(req.Offset)
	if err != nil {
		return nil, err
	}
	return &log_v1.ConsumeResponse{Record: record}, nil
}

/*
ProduceStream implements a bidirectional streaming
RPC so the client can stream data into the server’s log and the server can tell
the client whether each request succeeded.
*/
func (s *grpcServer) ProduceStream(
	stream log_v1.Log_ProduceStreamServer,
) error {
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
func (s *grpcServer) ConsumeStream(
	req *log_v1.ConsumeRequest,
	stream log_v1.Log_ConsumeStreamServer,
) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		default:
			res, err := s.Consume(stream.Context(), req)
			switch err.(type) {
			case nil:
			case log_v1.ErrOffsetOutOfRange:
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
