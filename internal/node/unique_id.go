package node

import (
	"fmt"
	"time"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

// UniqueIdHandler handles the generate message and returns the generate_ok message with the globally unique id
func (s *Server) UniqueIdHandler(m maelstrom.Message) error {
	now := time.Now().UnixNano()
	counter := s.counter.Add(1)
	id := fmt.Sprintf("%s-%d-%d", s.Node.ID(), now, counter)

	return s.Node.Reply(m, map[string]any{
		"type": "generate_ok",
		"id":   id,
	})
}
