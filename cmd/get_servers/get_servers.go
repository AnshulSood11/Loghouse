package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	api "github.com/anshulsood11/loghouse/api/v1"
	"github.com/anshulsood11/loghouse/internal/loadbalance"
	"github.com/anshulsood11/loghouse/internal/security"
	"github.com/anshulsood11/loghouse/internal/test_util"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"log"
	"time"
)

func main() {
	addr := flag.String("addr", ":8400", "service address")
	flag.Parse()
	clientTLSConfig, err := security.SetupTLSConfig(security.TLSConfig{
		CertFile:      test_util.RootClientCertFile,
		KeyFile:       test_util.RootClientKeyFile,
		CAFile:        test_util.CAFile,
		Server:        false,
		ServerAddress: "127.0.0.1",
	})
	client := client(addr, clientTLSConfig)
	ctx, _ := context.WithTimeout(context.Background(), 30*time.Second)
	fmt.Println("Calling client.GetServers in get_servers.go")
	res, err := client.GetServers(ctx, &api.GetServersRequest{})
	if err != nil {
		fmt.Println("Error while calling client.GetServers in get_servers.go")
		log.Fatal(err)
	}
	fmt.Println("servers:")
	for _, server := range res.Servers {
		fmt.Printf("%s\n", server)
	}
}

func client(addr *string, tlsConfig *tls.Config) api.LogClient {
	tlsCreds := credentials.NewTLS(tlsConfig)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(tlsCreds)}

	fmt.Println("Calling grpc Dial to ", fmt.Sprintf("%s:///%s", loadbalance.Name, *addr))
	conn, err := grpc.Dial(fmt.Sprintf("%s:///%s", loadbalance.Name, *addr), opts...)
	//conn, err := grpc.Dial(*addr, opts...)
	//conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	fmt.Println("After calling grpc dial, err: ", err)
	if err != nil {
		log.Fatal(err)
	}
	client := api.NewLogClient(conn)
	fmt.Println("returning newly created LogClient")
	return client
}
