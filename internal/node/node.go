package node

import (
	"sync"
	"sync/atomic"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

type Server struct {
	Node *maelstrom.Node

	// challenge 2: unique id generation
	counter atomic.Int64

	// challenge 3: broadcast
	broadcastMu sync.RWMutex
	messages    map[int64]struct{}

	// The topology of the network
	// We don't need mutexes for this if we assume that the topology is static
	// or it is only set once at startup. If it is set dynamically, we need to use a RWMutex.
	neighbors []string
}

func NewServer() *Server {
	return &Server{
		Node:     maelstrom.NewNode(),
		messages: make(map[int64]struct{}),
	}
}

// addMessage adds the message to the set of seen messages
func (s *Server) addMessage(id int64) bool {
	s.broadcastMu.Lock()
	defer s.broadcastMu.Unlock()
	if _, seen := s.messages[id]; seen {
		return false
	}

	s.messages[id] = struct{}{}
	return true
}

// getBroadcastMessages returns the set of seen messages as a slice
func (s *Server) getBroadcastMessages() []int64 {
	s.broadcastMu.RLock()
	defer s.broadcastMu.RUnlock()

	// Create a copy
	messages := make([]int64, 0, len(s.messages))
	for id := range s.messages {
		messages = append(messages, id)
	}

	return messages
}
