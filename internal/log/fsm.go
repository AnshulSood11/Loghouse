package log

import (
	"bytes"
	api "github.com/anshulsood11/loghouse/api/v1"
	"github.com/hashicorp/raft"
	"google.golang.org/protobuf/proto"
	"io"
)

type fsm struct {
	log *Log
}

var _ raft.FSM = (*fsm)(nil)

type RequestType uint8

const (
	AppendRequestType RequestType = 0
)

// Apply is invoked by Raft after committing a log entry.
func (f fsm) Apply(log *raft.Log) interface{} {
	buf := log.Data
	reqType := RequestType(buf[0])
	switch reqType {
	case AppendRequestType:
		return f.applyAppend(buf[1:])
	}
	return nil
}

func (f *fsm) applyAppend(b []byte) interface{} {
	var req api.ProduceRequest
	err := proto.Unmarshal(b, &req)
	if err != nil {
		return err
	}
	offset, err := f.log.Append(req.Record)
	if err != nil {
		return err
	}
	return &api.ProduceResponse{Offset: offset}
}

// Snapshot returns an FSMSnapshot that represents a point-in-time snapshot of
// the FSM’s state.
func (f fsm) Snapshot() (raft.FSMSnapshot, error) {
	r := f.log.Reader()
	return &snapshot{reader: r}, nil
}

// Restore is called by Raft to restore an FSM from a snapshot.
func (f fsm) Restore(snapshot io.ReadCloser) error {
	b := make([]byte, lenWidth)
	var buf bytes.Buffer
	for i := 0; ; i++ {
		_, err := io.ReadFull(snapshot, b)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		size := int64(enc.Uint64(b))
		if _, err = io.CopyN(&buf, snapshot, size); err != nil {
			return err
		}
		record := &api.Record{}
		if err = proto.Unmarshal(buf.Bytes(), record); err != nil {
			return err
		}
		// The FSM must discard existing state to make sure its state will match the
		// leader’s replicated state, so we reset the log and configure its initial offset
		// to the first record’s offset we read from the snapshot so the log’s offsets match.
		if i == 0 {
			f.log.Config.Segment.InitialOffset = record.Offset
			if err := f.log.Reset(); err != nil {
				return err
			}
		}
		// Appending the records one-by-one to our new log
		if _, err = f.log.Append(record); err != nil {
			return err
		}
		buf.Reset()
	}
	return nil
}
