package log

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"github.com/hashicorp/raft"
	"net"
	"time"
)

type Config struct {
	Raft struct {
		raft.Config
		StreamLayer *StreamLayer
		Bootstrap   bool
	}
	Segment struct {
		MaxStoreBytes uint64
		MaxIndexBytes uint64
		InitialOffset uint64
	}
}

type StreamLayer struct {
	listener        net.Listener
	serverTLSConfig *tls.Config
	peerTLSConfig   *tls.Config
}

func NewStreamLayer(listener net.Listener,
	serverTLSConfig, peerTLSConfig *tls.Config) *StreamLayer {
	return &StreamLayer{
		listener:        listener,
		serverTLSConfig: serverTLSConfig,
		peerTLSConfig:   peerTLSConfig,
	}
}

const RaftRPC = 1

// Dial makes outgoing connections to other servers in the Raft cluster.
// When connect to a server, we write the RaftRPC byte to identify the
// connection type, so we can multiplex Raft on the same port as our Log
// gRPC requests.
func (s StreamLayer) Dial(address raft.ServerAddress,
	timeout time.Duration) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout}
	var conn, err = dialer.Dial("tcp", string(address))
	if err != nil {
		return nil, err
	}
	_, err = conn.Write([]byte{byte(RaftRPC)})
	if err != nil {
		return nil, err
	}
	if s.peerTLSConfig != nil {
		// making a TLS client-side connection
		return tls.Client(conn, s.peerTLSConfig), nil
	}
	return conn, nil
}

// Accept is the mirror of Dial. We accept the incoming connection and read the
// byte that identifies the connection and then create a server-side TLS connection
func (s StreamLayer) Accept() (net.Conn, error) {
	// listener.Accept() waits for and returns the next connection to the listener.
	conn, err := s.listener.Accept()
	if err != nil {
		return nil, err
	}
	b := make([]byte, 1)
	_, err = conn.Read(b)
	if err != nil {
		return nil, err
	}
	if bytes.Compare([]byte{byte(RaftRPC)}, b) != 0 {
		return nil, fmt.Errorf("not a raft rpc")
	}
	if s.serverTLSConfig != nil {
		return tls.Server(conn, s.serverTLSConfig), nil
	}
	return conn, nil
}

// Close closes the listener
func (s StreamLayer) Close() error {
	return s.listener.Close()
}

// Addr returns the listener’s address
func (s StreamLayer) Addr() net.Addr {
	return s.listener.Addr()
}
