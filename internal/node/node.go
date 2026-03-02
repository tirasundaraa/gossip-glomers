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

	// challenge 3d: broadcast efficiency
	// The Buffer: Node ID -> Set of pending messages
	// e.g: "n2" -> {42: struct{}, 43: struct{}}
	pendingMu       sync.RWMutex
	pendingMessages map[string]map[int64]struct{}
}

func NewServer() *Server {
	return &Server{
		Node:            maelstrom.NewNode(),
		messages:        make(map[int64]struct{}),
		pendingMessages: make(map[string]map[int64]struct{}),
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

func (s *Server) addPendingMessage(neighborID string, msg int64) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	if s.pendingMessages[neighborID] == nil {
		s.pendingMessages[neighborID] = make(map[int64]struct{})
	}

	s.pendingMessages[neighborID][msg] = struct{}{}
}

func (s *Server) extractPending(neighborID string) []int64 {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	queue, exists := s.pendingMessages[neighborID]
	if !exists || len(queue) == 0 {
		return nil
	}

	pendingMessages := make([]int64, 0, len(queue))
	for id := range queue {
		pendingMessages = append(pendingMessages, id)
	}

	// NUKE AND REPLACE:
	// Instantly clears the bucket for this neighbor in O(1) time
	s.pendingMessages[neighborID] = make(map[int64]struct{})

	return pendingMessages
}

func (s *Server) requeueMessages(neighborID string, failedBatch []int64) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	// Just add them back! If 44 arrived while we were gone,
	// it stays perfectly safe. The queue becomes [42, 43, 44].
	for _, id := range failedBatch {
		if s.pendingMessages[neighborID] == nil {
			s.pendingMessages[neighborID] = make(map[int64]struct{})
		}

		s.pendingMessages[neighborID][id] = struct{}{}
	}
}

func (s *Server) propagate(id int64, excludeNode string) {
	for _, neighborID := range s.neighbors {
		if neighborID == excludeNode {
			continue
		}
		s.addPendingMessage(neighborID, id)
	}
}
