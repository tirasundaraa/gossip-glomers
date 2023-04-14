package main

import (
	"encoding/json"
	"log"
	"sync"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

func main() {
	n := maelstrom.NewNode()
	srv := &server{n: n, ids: make(map[int]struct{})}

	n.Handle("broadcast", srv.broadcastHandler)
	n.Handle("read", srv.readHandler)
	n.Handle("topology", srv.topologyHandler)

	if err := n.Run(); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	n *maelstrom.Node

	idsMu sync.RWMutex
	ids   map[int]struct{}
}

func (s *server) broadcastHandler(msg maelstrom.Message) error {
	var body map[string]any
	if err := json.Unmarshal(msg.Body, &body); err != nil {
		return err
	}

	id := int(body["message"].(float64))
	s.idsMu.Lock()
	if _, seen := s.ids[id]; seen {
		s.idsMu.Unlock()
		return nil // no need to broadcast. It seen already
	}
	s.ids[id] = struct{}{}
	s.idsMu.Unlock()

	// broadcast new data to other nodes
	for _, dst := range s.n.NodeIDs() {
		if dst == s.n.ID() || dst == msg.Src {
			continue
		}

		if err := s.n.Send(dst, body); err != nil {
			return err
		}
	}

	return s.n.Reply(msg, map[string]any{
		"type": "broadcast_ok",
	})
}

func (s *server) readHandler(msg maelstrom.Message) error {

	ids := make([]int, 0)
	s.idsMu.RLock()
	for k, _ := range s.ids {
		ids = append(ids, k)
	}
	s.idsMu.RUnlock()

	return s.n.Reply(msg, map[string]any{
		"type":     "read_ok",
		"messages": ids,
	})
}

func (s *server) topologyHandler(msg maelstrom.Message) error {

	return s.n.Reply(msg, map[string]any{
		"type": "topology_ok",
	})
}
