package discovery

import (
	"github.com/hashicorp/serf/serf"
	"go.uber.org/zap"
	"net"
)

/*
Membership is our type wrapping Serf to provide discovery and cluster membership
to our service. Users will call NewMembership() to create a Membership with the required
configuration and event handler.
*/
type Membership struct {
	Config
	handler Handler
	serf    *serf.Serf
	events  chan serf.Event
	logger  *zap.Logger
}

type Config struct {
	NodeName       string
	BindAddr       string
	Tags           map[string]string
	StartJoinAddrs []string
}

type Handler interface {
	Join(name, addr string) error
	Leave(name string) error
}

func NewMembership(handler Handler, config Config) (*Membership, error) {
	c := &Membership{
		Config:  config,
		handler: handler,
		logger:  zap.L().Named("membership"),
	}
	if err := c.setupSerf(); err != nil {
		return nil, err
	}
	return c, nil
}

/*
setupSerf() creates and configures a Serf instance and starts the eventsHandler()
goroutine to handle Serf’s events
*/
func (m *Membership) setupSerf() (err error) {
	addr, err := net.ResolveTCPAddr("tcp", m.BindAddr)
	if err != nil {
		return err
	}
	config := serf.DefaultConfig()
	config.Init()
	// Serf listens on this address and port for gossiping.
	config.MemberlistConfig.BindAddr = addr.IP.String()
	config.MemberlistConfig.BindPort = addr.Port
	m.events = make(chan serf.Event)
	// The event channel receives Serf’s events when a node joins or leaves the cluster
	config.EventCh = m.events
	// These tags are shared to other nodes in the cluster to inform how to handle this node,
	// like if it's a voter or a non-voter, sharing RPC addresses etc.
	config.Tags = m.Tags
	// Node name acts as the node’s unique identifier across the Serf cluster. Default is hostname.
	config.NodeName = m.Config.NodeName
	m.serf, err = serf.Create(config)
	if err != nil {
		return err
	}
	go m.eventHandler()
	// StartJoinAddrs is set to the addresses of nodes already in the cluster,
	// to enable this node to join the cluster.
	if m.StartJoinAddrs != nil {
		_, err = m.serf.Join(m.StartJoinAddrs, true)
		if err != nil {
			return err
		}
	}
	return nil
}

/*
eventHandler() runs in a loop reading events sent by Serf into the events channel,
handling each incoming event according to the event’s type. Serf may coalesce multiple
members updates into one event, so we iterate over event's members.
*/
func (m *Membership) eventHandler() {
	for e := range m.events {
		switch e.EventType() {
		case serf.EventMemberJoin:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					continue
				}
				if err := m.handler.Join(member.Name, member.Tags["rpc_addr"]); err != nil {
					m.logError(err, "failed to join", member)
				}
			}
		case serf.EventMemberLeave:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					return
				}
				if err := m.handler.Leave(member.Name); err != nil {
					m.logError(err, "failed to leave", member)
				}
			}
		}
	}
}

func (m *Membership) logError(err error, msg string, member serf.Member) {
	m.logger.Error(msg, zap.Error(err), zap.String("name", member.Name),
		zap.String("rpc_addr", member.Tags["rpc_addr"]))
}

func (m *Membership) isLocal(member serf.Member) bool {
	return m.serf.LocalMember().Name == member.Name
}

func (m *Membership) Members() []serf.Member {
	return m.serf.Members()
}
func (m *Membership) Leave() error {
	return m.serf.Leave()
}
