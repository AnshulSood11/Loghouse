package log

import (
	"github.com/hashicorp/raft"
	"io"
)

type snapshot struct {
	reader io.Reader
}

var _ raft.FSMSnapshot = (*snapshot)(nil)

// Persist is called by Raft to write its state to some sink like in-memory, a file, S3 etc.
func (s snapshot) Persist(sink raft.SnapshotSink) error {
	if _, err := io.Copy(sink, s.reader); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

// Release is called by Raft when it’s finished taking the snapshot
func (s snapshot) Release() {
}
