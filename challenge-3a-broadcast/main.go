package main

import (
	"encoding/json"
	"log"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

func main() {
	n := maelstrom.NewNode()
	srv := &server{n: n}
	n.Handle("broadcast", srv.broadcastHandler)
	n.Handle("read", srv.readHandler)
	n.Handle("topology", srv.topologyHandler)

	if err := n.Run(); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	n   *maelstrom.Node
	ids []float64
}

func (s *server) broadcastHandler(msg maelstrom.Message) error {
	var body map[string]any
	if err := json.Unmarshal(msg.Body, &body); err != nil {
		return err
	}

	s.ids = append(s.ids, body["message"].(float64))

	return s.n.Reply(msg, map[string]any{
		"type": "broadcast_ok",
	})
}

func (s *server) readHandler(msg maelstrom.Message) error {

	return s.n.Reply(msg, map[string]any{
		"type":     "read_ok",
		"messages": s.ids,
	})
}

func (s *server) topologyHandler(msg maelstrom.Message) error {

	return s.n.Reply(msg, map[string]any{
		"type": "topology_ok",
	})
}
