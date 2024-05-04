package log

import (
	"bytes"
	"fmt"
	api "github.com/anshulsood11/loghouse/api/v1"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
	"google.golang.org/protobuf/proto"
	"os"
	"path/filepath"
	"time"
)

type DistributedLog struct {
	config Config
	log    *Log
	raft   *raft.Raft
}

func NewDistributedLog(dataDir string, config Config) (*DistributedLog, error) {
	l := &DistributedLog{config: config}
	if err := l.setupLog(dataDir); err != nil {
		return nil, err
	}
	if err := l.setupRaft(dataDir); err != nil {
		return nil, err
	}
	return l, nil
}

/*
setupLog(dataDir string) creates the log for this server, where this server will store
the user’s records.
*/
func (l *DistributedLog) setupLog(dataDir string) error {
	logDir := filepath.Join(dataDir, "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	var err error
	l.log, err = NewLog(logDir, l.config)
	return err
}

func (l *DistributedLog) setupRaft(dataDir string) error {
	// Finite-state machine that applies the commands given
	fsm := &fsm{log: l.log}
	logDir := filepath.Join(dataDir, "raft", "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	logConfig := l.config
	logConfig.Segment.InitialOffset = 1
	// Log Store where Raft stores the given commands. Using our own log implementation.
	// Initial Offset is set to 1 as it is required by Raft.
	logStore, err := newLogStore(logDir, logConfig)
	if err != nil {
		return err
	}
	// Stable Store is a K-V store where Raft stores the metadata (like the server’s current
	// term or the candidate the server voted for)
	stableStore, err := raftboltdb.NewBoltStore(
		filepath.Join(dataDir, "raft", "stable"),
	)
	if err != nil {
		return err
	}
	retain := 1
	// Snapshot Store is where Raft stores compact snapshots of its data
	// variable retain specifies the number of previous snapshots to retain
	snapshotStore, err := raft.NewFileSnapshotStore(
		filepath.Join(dataDir, "raft"),
		retain,
		os.Stderr,
	)
	if err != nil {
		return err
	}
	maxPool := 5
	timeout := 10 * time.Second
	// Raft uses transport to communicate with other nodes
	transport := raft.NewNetworkTransport(
		l.config.Raft.StreamLayer,
		maxPool,
		timeout,
		os.Stderr,
	)
	config := raft.DefaultConfig()
	config.LocalID = l.config.Raft.LocalID
	if l.config.Raft.HeartbeatTimeout != 0 {
		config.HeartbeatTimeout = l.config.Raft.HeartbeatTimeout
	}
	if l.config.Raft.ElectionTimeout != 0 {
		config.ElectionTimeout = l.config.Raft.ElectionTimeout
	}
	if l.config.Raft.LeaderLeaseTimeout != 0 {
		config.LeaderLeaseTimeout = l.config.Raft.LeaderLeaseTimeout
	}
	if l.config.Raft.CommitTimeout != 0 {
		config.CommitTimeout = l.config.Raft.CommitTimeout
	}
	l.raft, err = raft.NewRaft(
		config,
		fsm,
		logStore,
		stableStore,
		snapshotStore,
		transport,
	)
	if err != nil {
		return err
	}
	hasState, err := raft.HasExistingState(
		logStore,
		stableStore,
		snapshotStore,
	)
	if err != nil {
		return err
	}
	// Only when the server is the only voter, then we bootstrap. The subsequently added
	// servers don't bootstrap.
	if l.config.Raft.Bootstrap && !hasState {
		config := raft.Configuration{
			Servers: []raft.Server{{
				ID:      config.LocalID,
				Address: transport.LocalAddr(),
			}},
		}
		err = l.raft.BootstrapCluster(config).Error()
	}
	return err
}

func (l *DistributedLog) Append(record *api.Record) (uint64, error) {
	res, err := l.apply(
		AppendRequestType,
		&api.ProduceRequest{Record: record},
	)
	if err != nil {
		return 0, err
	}
	return res.(*api.ProduceResponse).Offset, nil
}

func (l *DistributedLog) apply(reqType RequestType, req proto.Message) (
	interface{},
	error,
) {
	var buf bytes.Buffer
	_, err := buf.Write([]byte{byte(reqType)})
	if err != nil {
		return nil, err
	}
	b, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	_, err = buf.Write(b)
	if err != nil {
		return nil, err
	}
	timeout := 10 * time.Second
	future := l.raft.Apply(buf.Bytes(), timeout)
	if future.Error() != nil {
		return nil, future.Error()
	}
	res := future.Response()
	if err, ok := res.(error); ok {
		return nil, err
	}
	return res, nil
}

func (l *DistributedLog) Read(offset uint64) (*api.Record, error) {
	return l.log.Read(offset)
}

// Join adds the server to the Raft cluster. Every server is added as a voter.
// Servers can be added as a non-voters as well which are useful to replicate
// the state to multiple servers to serve read only eventually consistent state.
// Adding more voter servers makes replications and elections slower because
// the leader has more servers to communicate with to reach a majority. This must
// be run on the leader, or it will fail
func (l *DistributedLog) Join(id, addr string) error {
	configFuture := l.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return err
	}
	joinedServerID := raft.ServerID(id)
	joinedServerAddr := raft.ServerAddress(addr)

	for _, server := range configFuture.Configuration().Servers {
		if server.ID == joinedServerID && server.Address == joinedServerAddr {
			// server has already joined
			return nil
		}
		if server.ID == joinedServerID || server.Address == joinedServerAddr {
			// remove any existing server with serverID and address combination not matching
			removeFuture := l.raft.RemoveServer(joinedServerID, 0, 0)
			if err := removeFuture.Error(); err != nil {
				return err
			}
		}
	}
	addFuture := l.raft.AddVoter(joinedServerID, joinedServerAddr, 0, 0)
	if err := addFuture.Error(); err != nil {
		return err
	}
	return nil
}

// Leave removes the server from the cluster. Removing the leader will
// trigger a new election.
func (l *DistributedLog) Leave(id string) error {
	removeFuture := l.raft.RemoveServer(raft.ServerID(id), 0, 0)
	return removeFuture.Error()
}

// WaitForLeader blocks until the cluster has elected a leader or times out.
// It’s useful when writing tests
func (l *DistributedLog) WaitForLeader(timeout time.Duration) error {
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	select {
	case <-timeoutCh:
		return fmt.Errorf("timed out")
	case <-ticker.C:
		if _, currLeaderID := l.raft.LeaderWithID(); currLeaderID != "" {
			return nil
		}
	}
	return nil
}

func (l *DistributedLog) Close() error {
	f := l.raft.Shutdown()
	if err := f.Error(); err != nil {
		return err
	}
	return l.log.Close()
}
