package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	api "github.com/anshulsood11/loghouse/api/v1"
	"github.com/anshulsood11/loghouse/internal/test_util"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"io/ioutil"
	"os"
	"testing"
	"time"
)

func TestAgent(t *testing.T) {
	serverTLSConfig, err := test_util.SetupTLSConfig(test_util.TLSConfig{
		CertFile:      test_util.ServerCertFile,
		KeyFile:       test_util.ServerKeyFile,
		CAFile:        test_util.CAFile,
		Server:        true,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)
	peerTLSConfig, err := test_util.SetupTLSConfig(test_util.TLSConfig{
		CertFile:      test_util.RootClientCertFile,
		KeyFile:       test_util.RootClientKeyFile,
		CAFile:        test_util.CAFile,
		Server:        false,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)

	var agents []*Agent
	for i := 0; i < 3; i++ {
		ports := test_util.GetFreePorts(2)
		bindAddr := fmt.Sprintf("%s:%d", "127.0.0.1", ports[0])
		rpcPort := ports[1]

		dataDir, err := ioutil.TempDir("", "agent-test-log")
		require.NoError(t, err)
		var startJoinAddrs []string
		if i != 0 {
			startJoinAddrs = append(startJoinAddrs, agents[0].Config.BindAddr)
		}

		newAgent, err := NewAgent(Config{
			Bootstrap:       i == 0,
			NodeName:        fmt.Sprintf("%d", i),
			StartJoinAddrs:  startJoinAddrs,
			BindAddr:        bindAddr,
			RPCPort:         rpcPort,
			DataDir:         dataDir,
			ACLModelFile:    test_util.ACLModelFile,
			ACLPolicyFile:   test_util.ACLPolicyFile,
			ServerTLSConfig: serverTLSConfig,
			PeerTLSConfig:   peerTLSConfig,
		})
		require.NoError(t, err)
		agents = append(agents, newAgent)
	}

	defer func() {
		for _, a := range agents {
			err := a.Shutdown()
			require.NoError(t, err)
			require.NoError(t, os.RemoveAll(a.Config.DataDir))
		}
	}()

	// wait for nodes to discover each other
	time.Sleep(3 * time.Second)

	leaderClient := client(t, agents[0], peerTLSConfig)
	produceResponse, err := leaderClient.Produce(
		context.Background(),
		&api.ProduceRequest{
			Record: &api.Record{
				Value: []byte("foo"),
			},
		},
	)
	require.NoError(t, err)
	consumeResponse, err := leaderClient.Consume(
		context.Background(),
		&api.ConsumeRequest{
			Offset: produceResponse.Offset,
		},
	)
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Record.Value, []byte("foo"))

	// wait until replication has finished
	time.Sleep(3 * time.Second)

	// checking if log is successfully replicated
	followerClient := client(t, agents[1], peerTLSConfig)
	consumeResponse, err = followerClient.Consume(
		context.Background(),
		&api.ConsumeRequest{
			Offset: produceResponse.Offset,
		},
	)
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Record.Value, []byte("foo"))

	// checking if log is not replicated twice/infinitely
	consumeResponse, err = leaderClient.Consume(
		context.Background(),
		&api.ConsumeRequest{
			Offset: produceResponse.Offset + 1,
		},
	)
	require.Nil(t, consumeResponse)
	require.Error(t, err)
	require.Equal(t, status.Code(err), status.Code(api.ErrOffsetOutOfRange{}.GRPCStatus().Err()))
}

func client(t *testing.T, agent *Agent, tlsConfig *tls.Config) api.LogClient {
	tlsCreds := credentials.NewTLS(tlsConfig)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(tlsCreds)}
	rpcAddr, err := Config.RPCAddr(agent.Config)
	require.NoError(t, err)
	conn, err := grpc.Dial(fmt.Sprintf("%s", rpcAddr), opts...)
	require.NoError(t, err)
	client := api.NewLogClient(conn)
	return client
}
