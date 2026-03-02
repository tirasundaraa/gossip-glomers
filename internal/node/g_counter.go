package node

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

func (s *Server) ReadGCounterHandler(m maelstrom.Message) error {
	var total atomic.Int64
	var isFailed atomic.Bool

	wg := &sync.WaitGroup{}
	allNodes := s.Node.NodeIDs()
	parentCtx, parentCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer parentCancel()

	for _, peerNodeID := range allNodes {
		wg.Add(1)

		go func(ctx context.Context, waitGr *sync.WaitGroup, nodeID string) {
			defer waitGr.Done()

			for {

				readCtx, readCancel := context.WithTimeout(ctx, 1*time.Second)
				val, err := s.KV.ReadInt(readCtx, nodeID)
				readCancel()

				// All good!
				if err == nil {
					total.Add(int64(val))
					break
				}

				// The key doesn't exist. Nothing todo; not our fault!
				if rpcErr, ok := err.(*maelstrom.RPCError); ok && rpcErr.Code == maelstrom.KeyDoesNotExist {
					break
				}

				// The Network failed. Let's wait and retry!
				select {
				case <-ctx.Done():
					isFailed.Store(true)
					return
				case <-time.After(100 * time.Millisecond):
					continue // Okay, let's try again.
				}

			}
		}(parentCtx, wg, peerNodeID)
	}

	wg.Wait()
	if isFailed.Load() {
		return fmt.Errorf("read timed out: could not fetch all counter segments")
	}

	return s.Node.Reply(m, map[string]any{
		"type":  "read_ok",
		"value": total.Load(),
	})
}

func (s *Server) AddGCounterHandler(m maelstrom.Message) error {
	var body struct {
		Type  string `json:"type"`
		Delta int64  `json:"delta"`
	}
	var currentTotal int64
	var myNodeID string = s.Node.ID()
	parentCtx, parentCancel := context.WithTimeout(context.Background(), 5*time.Second) // MASTER CANCEL context
	defer parentCancel()

	if err := json.Unmarshal(m.Body, &body); err != nil {
		return err
	}

	s.kvMu.Lock()
	defer s.kvMu.Unlock()

	// read ops
	currentTotal, err := s.readKVInt64WithRetry(parentCtx, myNodeID)
	if err != nil {
		return err
	}

	// write ops
	newValue := currentTotal + body.Delta
	for {
		ctx, cancel := context.WithTimeout(parentCtx, 1*time.Second)
		err := s.KV.CompareAndSwap(ctx, myNodeID, currentTotal, newValue, true)
		cancel()

		if err == nil {
			break
		}

		if rpcErr, ok := err.(*maelstrom.RPCError); ok {
			if rpcErr.Code == maelstrom.PreconditionFailed {
				currentTotal, err = s.readKVInt64WithRetry(ctx, myNodeID)
				if err != nil {
					return err
				}
				newValue = currentTotal + body.Delta
			}

			if rpcErr.Code != maelstrom.TemporarilyUnavailable && rpcErr.Code != maelstrom.Timeout {
				return err // The error isn't retry-able. Abort!
			}
		}

		select {
		case <-parentCtx.Done():
			return fmt.Errorf("s.KV.Write: timed out: %v", err)
		case <-time.After(100 * time.Millisecond):
			continue
		}

	}

	return s.Node.Reply(m, map[string]any{
		"type": "add_ok",
	})
}

func (s *Server) readKVInt64WithRetry(ctx context.Context, key string) (int64, error) {
	for {
		ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
		value, err := s.KV.ReadInt(ctx, key)
		cancel()

		if err == nil {
			return int64(value), nil
		}

		if rpcErr, ok := err.(*maelstrom.RPCError); ok && rpcErr.Code == maelstrom.KeyDoesNotExist {
			return 0, nil
		}

		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("s.readKVInt64WithRetry: timed out: %v", err)
		case <-time.After(100 * time.Millisecond):
			continue // try again
		}
	}
}
