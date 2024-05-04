package log

import (
	api "github.com/anshulsood11/loghouse/api/v1"
	"github.com/hashicorp/raft"
)

type logStore struct {
	*Log
}

var _ raft.LogStore = (*logStore)(nil)

func newLogStore(dir string, c Config) (*logStore, error) {
	log, err := NewLog(dir, c)
	if err != nil {
		return nil, err
	}
	return &logStore{log}, nil
}

func (l logStore) FirstIndex() (uint64, error) {
	return l.LowestOffset()
}

func (l logStore) LastIndex() (uint64, error) {
	off, err := l.HighestOffset()
	return off, err
}

func (l logStore) GetLog(index uint64, raftLog *raft.Log) error {
	logStoreLog, err := l.Read(index)
	if err != nil {
		return err
	}
	raftLog.Data = logStoreLog.Value
	raftLog.Index = logStoreLog.Offset
	raftLog.Type = raft.LogType(logStoreLog.Type)
	raftLog.Term = logStoreLog.Term
	return nil
}

func (l logStore) StoreLog(raftLog *raft.Log) error {
	return l.StoreLogs([]*raft.Log{raftLog})
}

func (l logStore) StoreLogs(raftLogs []*raft.Log) error {
	for _, raftLog := range raftLogs {
		if _, err := l.Append(&api.Record{
			Value: raftLog.Data,
			Term:  raftLog.Term,
			Type:  uint32(raftLog.Type),
		}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRange remove records that are old or stored in a snapshot
func (l logStore) DeleteRange(min, max uint64) error {
	return l.Truncate(max)
}
