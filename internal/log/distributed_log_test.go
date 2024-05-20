package log

import (
	"fmt"
	api "github.com/anshulsood11/loghouse/api/v1"
	"github.com/anshulsood11/loghouse/internal/test_util"
	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestMultipleNodes(t *testing.T) {
	var nodes []*DistributedLog
	// setting up a three node cluster
	nodeCount := 3
	ports := test_util.GetFreePorts(3)

	for i := 0; i < nodeCount; i++ {
		dataDir, err := ioutil.TempDir("", "distributed-log-test")
		require.NoError(t, err)
		defer func(dir string) {
			_ = os.RemoveAll(dir)
		}(dataDir)

		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ports[i]))
		require.NoError(t, err)
		config := Config{}
		config.Raft.StreamLayer = NewStreamLayer(listener, nil, nil)
		config.Raft.LocalID = raft.ServerID(strconv.Itoa(i))
		// shortening the default Raft timeout configs so that Raft elects the leader quickly
		config.Raft.HeartbeatTimeout = 50 * time.Millisecond
		config.Raft.ElectionTimeout = 50 * time.Millisecond
		config.Raft.LeaderLeaseTimeout = 50 * time.Millisecond
		config.Raft.CommitTimeout = 5 * time.Millisecond
		if i == 0 {
			config.Raft.Bootstrap = true
		}
		node, err := NewDistributedLog(dataDir, config)
		require.NoError(t, err)
		if i != 0 {
			err = nodes[0].Join(strconv.Itoa(i), listener.Addr().String())
			require.NoError(t, err)
		} else {
			err = node.WaitForLeader(3 * time.Second)
			require.NoError(t, err)
		}
		nodes = append(nodes, node)
	}

	records := []*api.Record{
		{Value: []byte("first")},
		{Value: []byte("second")},
	}

	// Appending some records to our leader server and checking that Raft
	// replicates the records to its followers.
	for _, record := range records {
		originalOffset, err := nodes[0].Append(record)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			// check whether the record is replicated to each node
			for j := 0; j < nodeCount; j++ {
				replicatedRecord, err := nodes[j].Read(originalOffset)
				if err != nil {
					return false
				}
				if replicatedRecord.Offset != originalOffset {
					return false
				}
			}
			return true
		},
			500*time.Millisecond,
			50*time.Millisecond)
	}

	// Checking that the leader stops replicating to a server that has left
	// cluster, while continuing to replicate to the existing servers.
	err := nodes[0].Leave("1")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	off, err := nodes[0].Append(&api.Record{
		Value: []byte("third"),
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	record, err := nodes[1].Read(off)
	require.IsType(t, api.ErrOffsetOutOfRange{}, err)
	require.Nil(t, record)

	record, err = nodes[2].Read(off)
	require.NoError(t, err)
	require.Equal(t, []byte("third"), record.Value)
	require.Equal(t, off, record.Offset)
}
