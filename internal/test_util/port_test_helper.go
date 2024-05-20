package test_util

import (
	"math/rand/v2"
	"net"
)

func GetFreePorts(n int) (ports []int) {
	port := rand.IntN(65000) + 1000
	for len(ports) < n {
		port++
		ln, err := listen(port)
		if err != nil {
			continue
		}
		err1 := ln.Close()
		if err != nil {
			panic(err1)
		}
		ports = append(ports, port)
	}

	return ports
}

func listen(port int) (*net.TCPListener, error) {
	return net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
}
