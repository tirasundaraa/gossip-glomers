package main

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

const retries = 100

func main() {
	n := maelstrom.NewNode()
	memstore := NewMemStore()
	srv := &server{n: n, store: memstore}

	n.Handle("broadcast", srv.broadcastHandler)
	n.Handle("read", srv.readHandler)
	n.Handle("topology", srv.topologyHandler)

	if err := n.Run(); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	n *maelstrom.Node

	store *MemStore
}

func (s *server) broadcastHandler(msg maelstrom.Message) error {
	var body map[string]any
	if err := json.Unmarshal(msg.Body, &body); err != nil {
		return err
	}

	id := int(body["message"].(float64))
	if s.store.IsExist(id) {
		return nil
	}

	s.store.Put(id, struct{}{})

	// broadcast new data to other nodes
	for _, dst := range s.n.NodeIDs() {
		if dst == s.n.ID() || dst == msg.Src {
			continue
		}

		go func(dest string) {
			var (
				ack   bool
				ackMu sync.Mutex
			)

			ackHandler := func(_ maelstrom.Message) error {
				ackMu.Lock()
				defer ackMu.Unlock()

				ack = true
				return nil
			}

			for i := 1; i <= retries && !ack; i++ {
				s.n.RPC(dest, body, ackHandler)

				// backoff delay
				dur := time.Duration(i) * 100
				time.Sleep(dur * time.Millisecond)
			}
		}(dst)
	}

	return s.n.Reply(msg, map[string]any{
		"type": "broadcast_ok",
	})
}

func (s *server) readHandler(msg maelstrom.Message) error {

	return s.n.Reply(msg, map[string]any{
		"type":     "read_ok",
		"messages": s.store.Keys(),
	})
}

func (s *server) topologyHandler(msg maelstrom.Message) error {

	return s.n.Reply(msg, map[string]any{
		"type": "topology_ok",
	})
}
