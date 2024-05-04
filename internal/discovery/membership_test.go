package discovery

import (
	"fmt"
	"github.com/anshulsood11/loghouse/internal/util"
	"github.com/hashicorp/serf/serf"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestMembership(t *testing.T) {
	m, handler := setupMember(t, nil)
	m, _ = setupMember(t, m)
	m, _ = setupMember(t, m)
	require.Eventually(t, func() bool {
		return 2 == len(handler.joins) &&
			3 == len(m[0].serf.Members()) &&
			0 == len(handler.leaves)
	}, 3*time.Second, 250*time.Millisecond)
	require.NoError(t, m[2].Leave())
	require.Eventually(t, func() bool {
		return 2 == len(handler.joins) &&
			3 == len(m[0].Members()) &&
			serf.StatusLeft == m[0].Members()[2].Status &&
			1 == len(handler.leaves)
	}, 3*time.Second, 250*time.Millisecond)
	require.Equal(t, fmt.Sprintf("%d", 2), <-handler.leaves)
}

func setupMember(t *testing.T, members []*Membership) ([]*Membership, *handler) {
	id := len(members)
	ports := util.GetFreePorts(1)
	addr := fmt.Sprintf("%s:%d", "127.0.0.1", ports[0])
	tags := map[string]string{
		"rpc_addr": addr,
	}
	c := Config{
		NodeName: fmt.Sprintf("%d", id),
		BindAddr: addr,
		Tags:     tags,
	}
	h := &handler{}
	h.joins = make(chan map[string]string, 3)
	h.leaves = make(chan string, 3)
	if len(members) != 0 {
		c.StartJoinAddrs = []string{
			members[0].BindAddr,
		}
	}
	m, err := NewMembership(h, c)
	require.NoError(t, err)
	members = append(members, m)
	return members, h
}

type handler struct {
	joins  chan map[string]string
	leaves chan string
}

func (h *handler) Join(id, addr string) error {
	if h.joins != nil {
		h.joins <- map[string]string{
			"id":   id,
			"addr": addr,
		}
	}
	return nil
}
func (h *handler) Leave(id string) error {
	if h.leaves != nil {
		h.leaves <- id
	}
	return nil
}
