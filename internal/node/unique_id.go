package node

import (
	"encoding/json"
	"fmt"
	"time"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

// UniqueIdHandler handles the generate message and returns the generate_ok message with the globally unique id
func (s *Server) UniqueIdHandler(m maelstrom.Message) error {
	now := time.Now().UnixNano()
	counter := s.counter.Add(1)
	id := fmt.Sprintf("%s-%d-%d", s.Node.ID(), now, counter)

	body := map[string]any{}
	if err := json.Unmarshal(m.Body, &body); err != nil {
		return err
	}

	body["type"] = "generate_ok"
	body["id"] = id

	return s.Node.Reply(m, body)
}
