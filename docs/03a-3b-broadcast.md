# Challenge 3a & 3b: Eventual Consistency and Gossip Protocols

## Overview

In these challenges, we transitioned from stateless, independent nodes to a stateful, interconnected distributed system. The goal was to build a system that receives broadcast messages, stores them, and eventually propagates them to all other nodes in the cluster.

## Challenge 3a: Single-Node State

Before building the network protocol, we needed a thread-safe way to store the broadcasted messages on a single node.

### The State Structure

We used a Go `map[int64]struct{}` to represent a Mathematical Set.

* **Why an empty struct?** A `struct{}` consumes zero bytes of memory in Go. We only care about the keys (the message IDs), so this is the most memory-efficient way to ensure uniqueness and prevent storing duplicate messages.
* **Concurrency Control:** Maelstrom processes messages concurrently via goroutines. To prevent data races, we wrapped the map in a `sync.RWMutex`.

```go
type Server struct {
    broadcastMu sync.RWMutex
    messages    map[int64]struct{}
    neighbors   []string
}

```

---

## Challenge 3b: Multi-Node Broadcast (The Gossip Protocol)

In 3b, the system was expanded to 5 nodes. When a node receives a new message, it must relay it to its direct `neighbors` defined by the cluster topology.

### 1. Cycle Detection & The TOCTOU Race Condition

The biggest danger in a gossip protocol is an infinite loop (e.g., Node A tells Node B, Node B tells Node A, and so on forever).

Initially, we had a Time-of-Check to Time-of-Use (TOCTOU) vulnerability:

1. Check if message is seen (`isMessageSeen`) -> Release Lock.
2. If not seen, add message (`addMessage`) -> Acquire Lock.

Under high concurrency, multiple goroutines could see the message as "unseen" simultaneously and broadcast it multiple times, causing a network storm. We fixed this by combining the check and the write into a single atomic operation under an exclusive write lock:

```go
func (s *Server) addMessage(id int64) bool {
    s.broadcastMu.Lock()
    defer s.broadcastMu.Unlock()
    if _, seen := s.messages[id]; seen {
        return false // Already knew about it, do nothing
    }
    s.messages[id] = struct{}{}
    return true // Genuinely new message, we should gossip it!
}

```

### 2. Network Optimizations

When a message is confirmed as new (`addMessage` returns `true`), we iterate through our `neighbors` to gossip it. To save network bandwidth, we explicitly skip sending the message back to the node that just sent it to us:

```go
if nodeID != m.Src {
    s.Node.Send(nodeID, ...)
}

```

### 3. The Peer vs. Client Routing Hack

Maelstrom requires a `broadcast_ok` reply for every `broadcast` request it sends as a *Client*. However, if our internal nodes reply `broadcast_ok` to each other during peer-to-peer gossip, Maelstrom crashes because it has no registered handler for unexpected `broadcast_ok` messages arriving at nodes.

We used a pragmatic heuristic to solve this: Maelstrom prefixes client IDs with `c` (e.g., `c1`) and node IDs with `n` (e.g., `n1`).

```go
// Only acknowledge the Client, remain silent to Peers
if m.Src[0] == 'n' {
    return nil 
}
return s.Node.Reply(m, map[string]any{"type": "broadcast_ok"})

```

This allowed us to use the fast, non-blocking "Fire-and-Forget" `s.Node.Send()` method for peer communication while still satisfying the test harness's client requirements.
