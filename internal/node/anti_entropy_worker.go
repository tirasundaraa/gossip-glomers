package node

import (
	"context"
	"time"
)

func (s *Server) StartGossipWorker() {
	ticker := time.NewTicker(50 * time.Millisecond)
	for range ticker.C {
		for _, neighborID := range s.Node.NodeIDs() {
			if s.Node.ID() == neighborID {
				continue // skip self
			}

			batch := s.extractPending(neighborID)
			if len(batch) == 0 {
				continue
			}

			gossipFn := func(neighborId string, messages []int64) {
				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
				defer cancel()
				_, err := s.Node.SyncRPC(ctx, neighborId, map[string]any{
					"type":     "gossip",
					"messages": messages,
				})
				if err != nil {
					s.requeueMessages(neighborId, messages)
				}
			}

			go gossipFn(neighborID, batch)
		}
	}
}
