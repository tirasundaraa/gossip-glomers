package main

import (
	"log"

	"github.com/tirasundaraa/gossip-glomers/internal/node"
)

func main() {
	s := node.NewServer()

	s.Node.Handle("echo", s.EchoHandler)
	s.Node.Handle("generate", s.UniqueIdHandler)

	if err := s.Node.Run(); err != nil {
		log.Fatal(err)
	}
}
