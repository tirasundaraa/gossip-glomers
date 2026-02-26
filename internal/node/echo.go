package node

import (
	"encoding/json"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

// EchoHandler handles the echo message and returns the echo_ok message
func (s *Server) EchoHandler(m maelstrom.Message) error {

	// Unmarshall the message body as a map[string]any
	var body map[string]any
	if err := json.Unmarshal(m.Body, &body); err != nil {
		return err
	}

	// Update message for the response type
	body["type"] = "echo_ok"

	// Reply using the library helper
	return s.Node.Reply(m, body)
}
