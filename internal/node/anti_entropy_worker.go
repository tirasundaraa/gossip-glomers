package node

import "time"

func (s *Server) StartGossipWorker() {
	ticker := time.NewTicker(500 * time.Millisecond)
	for range ticker.C {
		// 1. Get ALL messages I currently know about
		knownMessages := s.getBroadcastMessages()

		// 2. Skip if there is no message or no neighbors
		if len(knownMessages) == 0 || len(s.neighbors) == 0 {
			continue
		}

		// 3. Send my entire state to my neighbors
		for _, neighbor := range s.neighbors {
			s.Node.Send(neighbor, map[string]any{
				"type":     "sync_state", // a custom message type we invent
				"messages": knownMessages,
			})
		}
	}
}
