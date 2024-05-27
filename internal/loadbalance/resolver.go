package loadbalance

import (
	"context"
	"fmt"
	api "github.com/anshulsood11/loghouse/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"sync"
)

type Resolver struct {
	clientConn    resolver.ClientConn
	resolverConn  *grpc.ClientConn
	serviceConfig *serviceconfig.ParseResult
	logger        *zap.Logger
	mu            sync.Mutex
}

var _ resolver.Builder = (*Resolver)(nil)
var _ resolver.Resolver = (*Resolver)(nil)

const Name = "loghouse"

// Build receives the data (like the target address) needed to build a
// resolver that can discover the servers and update the client connection
// with the servers it discovers.
func (r *Resolver) Build(target resolver.Target, cc resolver.ClientConn,
	opts resolver.BuildOptions) (resolver.Resolver, error) {
	r.logger = zap.L().Named("resolver")
	// clientConn connection is the user’s client connection and gRPC passes it
	// to the resolver for the resolver to update with the servers it discovers.
	r.clientConn = cc
	var dialOpts []grpc.DialOption
	if opts.DialCreds != nil {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(opts.DialCreds))
	}
	// Services can specify how clients should balance their calls to the service
	// by updating the state with a service config. The state is updated with a
	// service config that specifies to use the “Loghouse” load balancer written
	// in our picker.go
	r.serviceConfig = r.clientConn.ParseServiceConfig(
		fmt.Sprintf(`{"loadBalancingConfig":[{"%s":{}}]}`, Name),
	)
	var err error
	// resolverConn is the resolver’s own client connection to the server so it
	// can call GetServers() and get the servers.
	r.resolverConn, err = grpc.Dial(target.Endpoint(), dialOpts...)
	if err != nil {
		return nil, err
	}
	r.ResolveNow(resolver.ResolveNowOptions{})
	return r, nil
}

// Scheme returns the resolver’s scheme identifier. When grpc.Dial
// is called, gRPC parses out the scheme from the target address given
// and tries to find a resolver that matches, defaulting to its DNS
// resolver. For this resolver, target address will be formatted like
// this: loghouse://our-service-address.
func (r *Resolver) Scheme() string {
	return Name
}

// ResolveNow is used by gRPC to resolve the target, discover the servers,
// and update the client connection with the servers.
func (r *Resolver) ResolveNow(resolver.ResolveNowOptions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	client := api.NewLogClient(r.resolverConn)
	ctx := context.Background()
	res, err := client.GetServers(ctx, &api.GetServersRequest{})
	if err != nil {
		r.logger.Error("failed to resolve server", zap.Error(err))
		return
	}
	var addrs []resolver.Address
	for _, server := range res.Servers {
		addrs = append(addrs, resolver.Address{
			Addr:       server.RpcAddr,
			Attributes: attributes.New("is_leader", server.IsLeader),
		})
	}
	err = r.clientConn.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: r.serviceConfig,
	})
	if err != nil {
		r.logger.Error("failed to update state", zap.Error(err))
		return
	}
}

func (r *Resolver) Close() {
	if err := r.resolverConn.Close(); err != nil {
		r.logger.Error("failed to close conn", zap.Error(err))
	}
}

func init() {
	// register this resolver with gRPC
	resolver.Register(&Resolver{})
}
