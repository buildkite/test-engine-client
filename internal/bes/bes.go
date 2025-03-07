package bes

import (
	"fmt"
	"net"
)

var host = "127.0.0.1"
var port = 60242 // 0 for OS-allocated

func Listen() error {
	addr := fmt.Sprintf("%s:%d", host, port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	fmt.Println("Bazel BES listener: grpc://" + listener.Addr().String())

	// TODO

	return nil
}
