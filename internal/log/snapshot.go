package log

import (
	"github.com/hashicorp/raft"
	"io"
)

type Snapshot struct {
	reader io.Reader
}

var _ raft.FSMSnapshot = (*Snapshot)(nil)

// Persist is called by Raft to write its state to some sink like in-memory, a file, S3 etc.
func (s Snapshot) Persist(sink raft.SnapshotSink) error {
	if _, err := io.Copy(sink, s.reader); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

// Release is called by Raft when it’s finished taking the Snapshot
func (s Snapshot) Release() {
}
