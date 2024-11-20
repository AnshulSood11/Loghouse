package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/anshulsood11/loghouse/internal/security"
	"github.com/anshulsood11/loghouse/internal/test_util"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"log"
	"os"
)

func main() {
	exitCode := 1
	defer func() { os.Exit(exitCode) }()
	addr := flag.String("addr", ":8400", "service address")
	flag.Parse()
	clientTLSConfig, err := security.SetupTLSConfig(security.TLSConfig{
		CertFile:      test_util.RootClientCertFile,
		KeyFile:       test_util.RootClientKeyFile,
		CAFile:        test_util.CAFile,
		Server:        false,
		ServerAddress: "127.0.0.1",
	})
	ctx := context.Background()
	healthClient := healthClient(addr, clientTLSConfig)
	healthCheckResponse, err := healthClient.Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("healthCheckResponse: %s\n", healthCheckResponse)
	if healthCheckResponse.Status == healthpb.HealthCheckResponse_SERVING {
		exitCode = 0
	}
}

func healthClient(addr *string, tlsConfig *tls.Config) healthpb.HealthClient {
	tlsCreds := credentials.NewTLS(tlsConfig)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(tlsCreds)}
	conn, err := grpc.Dial(*addr, opts...)
	if err != nil {
		log.Fatal(err)
	}
	client := healthpb.NewHealthClient(conn)
	return client
}
