package main

import (
	"log"

	"github.com/tirasundaraa/gossip-glomers/internal/node"
)

func main() {
	s := node.NewServer()

	s.Node.Handle("echo", s.EchoHandler)
	s.Node.Handle("generate", s.UniqueIdHandler)
	s.Node.Handle("broadcast", s.BroadcastHandler)
	//s.Node.Handle("read", s.ReadHandler)  // comment this out. we need to introduce another "read" request for challenge 4.
	s.Node.Handle("topology", s.TopologyHandler)
	s.Node.Handle("gossip", s.GossipHandler)

	// challenge 4: Grow-only Counter
	s.Node.Handle("add", s.AddGCounterHandler)
	s.Node.Handle("read", s.ReadGCounterHandler)

	// Start the background worker for the anti-entropy synchronization
	go s.StartGossipWorker()

	if err := s.Node.Run(); err != nil {
		log.Fatal(err)
	}
}
