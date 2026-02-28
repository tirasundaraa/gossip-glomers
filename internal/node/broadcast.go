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
		s.propagate(id, m.Src)
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
	allNodes := s.Node.NodeIDs()
	myID := s.Node.ID()
	var myIndex int
	for i, id := range allNodes {
		if id == myID {
			myIndex = i
			break
		}
	}

	s.neighbors = []string{}
	fanOut := 10 // <--- The magic number

	if myIndex > 0 {
		parentIndex := (myIndex - 1) / fanOut
		s.neighbors = append(s.neighbors, allNodes[parentIndex])
	}

	for j := 1; j <= fanOut; j++ {
		childIndex := (myIndex * fanOut) + j
		if childIndex < len(allNodes) {
			s.neighbors = append(s.neighbors, allNodes[childIndex])
		}
	}

	return s.Node.Reply(m, map[string]any{"type": "topology_ok"})
}

func (s *Server) GossipHandler(m maelstrom.Message) error {
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

		if s.addMessage(id) {
			s.propagate(id, m.Src)
		}
	}

	return s.Node.Reply(m, map[string]any{
		"type": "gossip_ok",
	})
}
