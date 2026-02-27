package node

import (
	"encoding/json"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

func (s *Server) BroadcastHandler(m maelstrom.Message) error {
	var body struct {
		Type    string      `json:"type"`
		Message json.Number `json:"message"`
	}

	if err := json.Unmarshal(m.Body, &body); err != nil {
		return err
	}

	id, err := body.Message.Int64()
	if err != nil {
		return err
	}

	if s.addMessage(id) {
		// Broadcast to the neighbors
		for _, nodeID := range s.neighbors {
			if nodeID != m.Src {
				s.Node.Send(nodeID, map[string]any{
					"type":    "broadcast",
					"message": id,
				})
			}
		}
	}

	// no need to reply to broadcast from other Nodes
	if m.Src[0] == 'n' {
		return nil
	}

	return s.Node.Reply(m, map[string]any{
		"type": "broadcast_ok",
	})
}

func (s *Server) ReadHandler(m maelstrom.Message) error {
	var body map[string]any

	if err := json.Unmarshal(m.Body, &body); err != nil {
		return err
	}

	body["type"] = "read_ok"
	body["messages"] = s.getBroadcastMessages()
	return s.Node.Reply(m, body)
}

// TopologyHandler handles the topology message and updates the neighbors
func (s *Server) TopologyHandler(m maelstrom.Message) error {
	var body struct {
		Type     string              `json:"type"`
		Topology map[string][]string `json:"topology"`
	}

	if err := json.Unmarshal(m.Body, &body); err != nil {
		return err
	}

	if neighbors, ok := body.Topology[s.Node.ID()]; ok {
		s.neighbors = neighbors
	}

	return s.Node.Reply(m, map[string]any{
		"type": "topology_ok",
	})
}

func (s *Server) SyncStateHandler(m maelstrom.Message) error {

	var body struct {
		Type     string        `json:"type"`
		Messages []json.Number `json:"messages"`
	}

	if err := json.Unmarshal(m.Body, &body); err != nil {
		return err
	}

	for _, msg := range body.Messages {
		id, err := msg.Int64()
		if err != nil {
			return err
		}

		s.addMessage(id)
	}

	return nil
}
