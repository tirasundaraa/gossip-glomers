# Challenge 3c: Fault Tolerance (Surviving Network Partitions)

## Overview

In Challenge 3b, we built a gossip protocol assuming a perfectly reliable network. In Challenge 3c, the test harness introduced **Network Partitions** (`--nemesis partition`). Network cables were virtually cut, causing messages sent via our "fire-and-forget" `s.Node.Send()` method to be silently dropped.

Isolated nodes missed broadcast messages, leading to test failures when they were asked to `read` their state. We needed a mechanism to ensure messages *eventually* reached all nodes, even if they were temporarily disconnected.

## The Architecture Decision: Anti-Entropy vs. RPC Retries

To guarantee message delivery, we had two main architectural choices:

1. **Message-Centric (RPC Retries):** For every broadcast, send an RPC message and wait for an acknowledgment. If it times out, retry.
* *Drawback:* During a prolonged partition, this spawns thousands of sleeping/retrying goroutines, potentially causing memory leaks and blocking system resources.


2. **State-Centric (Anti-Entropy):** Run a single background process that periodically synchronizes the *entire known state* with neighbors.

We chose **Option 2: Anti-Entropy**. This is the same conceptual engine used in highly available distributed databases like Amazon DynamoDB and Apache Cassandra. It prioritizes system stability and predictable resource usage over immediate message tracking.

## Implementation Details

### 1. The Background Gossiper (`StartGossipWorker`)

We implemented a background goroutine that ticks every 500 milliseconds. Instead of worrying about individual dropped messages, it simply grabs the node's entire localized state and blasts it to all neighbors using a custom message type: `sync_state`.

```go
func (s *Server) StartGossipWorker() {
    ticker := time.NewTicker(500 * time.Millisecond)
    for range ticker.C {
        knownMessages := s.getBroadcastMessages()
        if len(knownMessages) == 0 || len(s.neighbors) == 0 {
            continue
        }

        // Blast entire state to neighbors
        for _, neighbor := range s.neighbors {
            s.Node.Send(neighbor, map[string]any{
                "type":     "sync_state",
                "messages": knownMessages,
            })
        }
    }
}

```

### 2. The Reconciliation Handler (`SyncStateHandler`)

When a node receives a `sync_state` message, it doesn't blindly overwrite its state. It iterates through the incoming array and passes each ID to our thread-safe `s.addMessage(id)` method.

Because `addMessage` is atomic, it naturally ignores messages the node already knows about and safely integrates the missing ones.

```go
func (s *Server) SyncStateHandler(m maelstrom.Message) error {
    // ... unmarshal JSON array of messages ...
    for _, msg := range body.Messages {
        id, _ := msg.Int64()
        s.addMessage(id) // Silently drops duplicates, adds new ones
    }
    return nil
}

```

## Key Concepts Demonstrated

### The CAP Theorem (Building an AP System)

This challenge forced us to deal with the **P** (Partition Tolerance) in the CAP theorem. We chose to build an **AP (Available and Partition-tolerant)** system:

* **Available:** Even when isolated, our nodes continued to accept `broadcast` writes and `read` queries from clients without returning errors.
* **Eventual Consistency:** Reads during a partition occasionally returned incomplete (stale) data. However, the moment the network healed, our Anti-Entropy worker immediately bridged the gap, syncing the missing arrays and bringing the cluster back into a globally consistent state.
