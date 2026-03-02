# Gossip Glomers - Project Research Report

## Overview

This is a Go implementation of the [Gossip Glomers](https://fly.io/dist-sys/) distributed systems challenges, built on top of the [Maelstrom](https://github.com/jepsen-io/maelstrom) testing framework (from the Jepsen ecosystem). The project implements distributed nodes that communicate via a JSON-over-STDIN/STDOUT protocol, and Maelstrom verifies their correctness under various failure conditions (network partitions, crashes, etc.).

The repository has two distinct layers of history: an older set of standalone challenge implementations (now in `archived/`), and a current, cleaner monolithic binary where all handlers live in one Go module.

---

## Repository Structure

```
gossip-glomers/
├── cmd/node/main.go                    # Single entry point - registers all handlers, runs event loop
├── internal/node/
│   ├── node.go                         # Server struct, shared state, pending-message machinery
│   ├── echo.go                         # Challenge 1: Echo handler
│   ├── unique_id.go                    # Challenge 2: Unique ID handler
│   ├── broadcast.go                    # Challenge 3: Broadcast, Read, Topology, Gossip handlers
│   └── anti_entropy_worker.go          # Challenge 3: Background gossip worker (anti-entropy)
├── archived/                           # Previous standalone implementations (historical)
│   ├── challenge-1-echo/
│   ├── challenge-2-unique-ids/
│   ├── challenge-3a-broadcast/
│   ├── challenge-3b-broadcast/
│   └── challenge-3c-broadcast/
├── docs/                               # Challenge write-ups and research
│   ├── 02-unique-ids.md
│   ├── 03a-3b-broadcast.md
│   ├── 03c-broadcast-fault-tolerant.md
│   ├── 03d-03e-broadcast-spanning-tree.md
│   ├── 03d-broadcast-efficiency.md
│   └── research.md                     # This file
├── maelstrom/                          # Vendored Maelstrom binary + Go library + docs (gitignored)
├── store/                              # Test result artifacts (gitignored)
│   ├── echo/
│   ├── unique-ids/
│   └── broadcast/
│       ├── <30+ timestamped test runs>/
│       └── latest -> <most recent run>
├── Makefile                            # Build + test automation
├── go.mod / go.sum                     # Go module (single dependency: jepsen-io/maelstrom)
└── .gitignore                          # Ignores maelstrom/, bin/, store/
```

---

## The Maelstrom Framework

### What It Is

Maelstrom is a workload generator and checker from the Jepsen distributed systems testing family. It spins up your binary as one or more OS processes, connects them via a simulated network, sends workload-specific requests, and then checks the resulting history for correctness violations.

### The Protocol

Communication follows a strict JSON-over-STDIO protocol:

1. **Messages** are JSON objects with `src`, `dest`, and `body` fields, separated by newlines on STDIN/STDOUT.
2. **Message bodies** have a mandatory `type` field, plus optional `msg_id` (unique per sender) and `in_reply_to` (for request-response pairing).
3. **Initialization**: Maelstrom sends an `init` message to each node containing its `node_id` (e.g., `"n0"`) and the full list of `node_ids` in the cluster. The node must reply with `init_ok`.
4. **Errors** use a structured `{"type": "error", "code": N, "text": "..."}` format with defined codes (0=timeout, 13=crash, 20=key-does-not-exist, 22=precondition-failed, etc.). Errors are classified as *definite* (operation definitely didn't happen) or *indefinite* (might have happened).

### The Go Library (`maelstrom/demo/go`)

The project depends on a single module: `github.com/jepsen-io/maelstrom/demo/go`. This library provides:

- **`Node`** - The core abstraction. Wraps STDIN/STDOUT, manages handler dispatch, callbacks, and message IDs. Every incoming message is dispatched to a handler in its own goroutine (concurrent by default).
- **`Node.Handle(type, fn)`** - Registers a handler for a message type.
- **`Node.Reply(msg, body)`** - Sends a response, automatically injecting `in_reply_to`.
- **`Node.Send(dest, body)`** - Fire-and-forget message to another node. Mutex-protected STDOUT writes.
- **`Node.RPC(dest, body, callback)`** - Async RPC: sends a message with a generated `msg_id`, registers a callback for the response.
- **`Node.SyncRPC(ctx, dest, body)`** - Synchronous RPC with context-based cancellation.
- **`KV`** - Client for Maelstrom's built-in key-value store services (`lin-kv`, `seq-kv`, `lww-kv`), providing `Read`, `Write`, and `CompareAndSwap`.

Key concurrency detail: the `Run()` loop reads messages sequentially from STDIN on the main goroutine, but dispatches every handler invocation to a new goroutine. This means handlers **must be thread-safe**. STDOUT writes are protected by a mutex inside `Send()`.

---

## Current Implementation

### Architecture

The current code is a single binary that handles all challenges (1, 2, 3a-3e). Maelstrom only sends message types relevant to the workload being tested, so unused handlers are inert.

- **`cmd/node/main.go`** - Entry point. Creates a `Server`, registers six handlers (`echo`, `generate`, `broadcast`, `read`, `topology`, `gossip`), starts the background gossip worker in a goroutine, then calls `Node.Run()` which blocks on the STDIN event loop.
- **`internal/node/node.go`** - Defines the `Server` struct with all shared state and the pending-message machinery (add, extract, requeue, propagate).
- **`internal/node/echo.go`** - Challenge 1: Echo handler.
- **`internal/node/unique_id.go`** - Challenge 2: Unique ID generation handler.
- **`internal/node/broadcast.go`** - Challenge 3: Broadcast, Read, Topology, and Gossip handlers.
- **`internal/node/anti_entropy_worker.go`** - Challenge 3: Background anti-entropy worker that periodically flushes batched messages to neighbors.

All handlers are methods on `*Server`, giving them shared access to the node, counter, messages, topology, and pending-message state.

### The `Server` Struct

```go
type Server struct {
    Node *maelstrom.Node

    // Challenge 2: unique id generation
    counter atomic.Int64

    // Challenge 3: broadcast
    broadcastMu sync.RWMutex
    messages    map[int64]struct{}

    // Topology (set once at startup, no mutex needed)
    neighbors []string

    // Challenge 3d: broadcast efficiency
    pendingMu       sync.RWMutex
    pendingMessages map[string]map[int64]struct{}
}
```

| Field | Type | Purpose | Synchronization |
|---|---|---|---|
| `Node` | `*maelstrom.Node` | Maelstrom protocol interface | Internally thread-safe |
| `counter` | `atomic.Int64` | Unique ID sequence number | Lock-free atomic ops |
| `messages` | `map[int64]struct{}` | Set of all seen broadcast values | `broadcastMu` (RWMutex) |
| `neighbors` | `[]string` | Tree topology neighbor list | Written once, read many (no mutex) |
| `pendingMessages` | `map[string]map[int64]struct{}` | Per-neighbor outbound message buffer | `pendingMu` (RWMutex) |

### Shared State Methods

Five helper methods on `*Server` manage the shared state:

1. **`addMessage(id) bool`** - Atomically checks-and-inserts a message into `messages`. Returns `true` if the message was genuinely new, `false` if already seen. This combined check-and-write under a single write lock eliminates the TOCTOU race condition that plagued the earlier 3b implementation.

2. **`getBroadcastMessages() []int64`** - Returns a snapshot copy of all seen messages under a read lock.

3. **`addPendingMessage(neighborID, msg)`** - Adds a message to the per-neighbor outbound buffer. Lazily initializes the inner map.

4. **`extractPending(neighborID) []int64`** - Atomically extracts all pending messages for a neighbor and replaces the bucket with a fresh empty map. This "nuke and replace" pattern is O(1) for the clear and avoids races between the worker extracting and handlers inserting new messages concurrently.

5. **`requeueMessages(neighborID, failedBatch)`** - Merges a failed batch back into the pending buffer. Safe even if new messages arrived in the interim.

6. **`propagate(id, excludeNode)`** - Queues a message for delivery to all neighbors except the sender, preventing infinite loops.

---

## Challenge 1: Echo

The simplest challenge. Receives a message, sets `type` to `"echo_ok"`, and replies. The entire original body (including the `echo` field) is preserved and sent back. This validates that the node can correctly participate in the Maelstrom protocol.

**Test parameters**: 1 node, 10 seconds.
**Results**: 47 operations, 100% success, 2.04 messages per operation (one request + one response).

---

## Challenge 2: Unique ID Generation

The core challenge here is generating **globally unique IDs** across multiple nodes, under network partitions, at high throughput, with no central coordinator.

**Strategy - Composite key**: `"{nodeID}-{unixNano}-{counter}"`

Three sources of entropy combine to guarantee uniqueness:

| Component | Purpose | Guarantees |
|---|---|---|
| `Node.ID()` (e.g., `"n0"`) | Spatial uniqueness | No two nodes share a prefix |
| `time.Now().UnixNano()` | Temporal uniqueness + crash safety | Post-crash IDs have a later timestamp than pre-crash IDs |
| `atomic.Int64.Add(1)` | Sub-nanosecond disambiguation | Strictly orders concurrent requests within the same nanosecond |

**Why not just a counter?** A crash would reset an in-memory counter to 0, causing duplicates. The timestamp ensures the "window" always moves forward.

**Why not just a timestamp?** Multiple requests can arrive within the same nanosecond on modern hardware. The atomic counter handles this.

**Why not just `NodeID + counter`?** Would work for single-run uniqueness, but crashes would reset the counter.

**Trade-offs**:
- String IDs are longer than 64-bit Snowflake IDs (less efficient to index in a DB).
- Clock dependency means IDs aren't strictly time-ordered across nodes (clock skew), though uniqueness is unaffected thanks to the NodeID prefix.

**Test parameters**: 3 nodes, 30 seconds, 1000 requests/second, total availability required, network partitions injected.
**Results**: 24,650 operations, 0 failures, 0 duplicates, 2.00 messages per operation. ID range from `"n0-..."` to `"n2-..."`.

---

## Challenge 3a: Single-Node Broadcast

Before building the network protocol, a thread-safe way to store broadcasted messages on a single node was needed.

**State structure**: A `map[int64]struct{}` (mathematical set) protected by `sync.RWMutex`. The empty struct consumes zero bytes; only the keys (message IDs) matter.

**Handlers**:
- `BroadcastHandler`: Stores the message value, replies `broadcast_ok`.
- `ReadHandler`: Returns all stored messages as `read_ok`.
- `TopologyHandler`: Acknowledges but topology is irrelevant for single-node.

**Test parameters**: 1 node, 20 seconds, 10 req/s.

---

## Challenge 3b: Multi-Node Broadcast (Gossip Protocol)

Expanded to 5 nodes. When a node receives a new message, it must relay it to its neighbors.

### Deduplication and the TOCTOU Fix

The biggest danger in a gossip protocol is infinite loops (A tells B, B tells A, forever). The `addMessage()` method combines check-and-write into a single atomic operation under an exclusive write lock:

```go
func (s *Server) addMessage(id int64) bool {
    s.broadcastMu.Lock()
    defer s.broadcastMu.Unlock()
    if _, seen := s.messages[id]; seen {
        return false
    }
    s.messages[id] = struct{}{}
    return true
}
```

Without this, a race condition exists: multiple goroutines could see the message as "unseen" simultaneously and broadcast it multiple times.

### Sender Exclusion

When propagating, the node skips the sender to reduce redundant messages:

```go
func (s *Server) propagate(id int64, excludeNode string) {
    for _, neighborID := range s.neighbors {
        if neighborID == excludeNode {
            continue
        }
        s.addPendingMessage(neighborID, id)
    }
}
```

**Test parameters**: 5 nodes, 20 seconds, 10 req/s.

---

## Challenge 3c: Fault-Tolerant Broadcast (Network Partitions)

The test harness introduces `--nemesis partition`, virtually cutting network cables. Fire-and-forget `Send()` messages get silently dropped, so isolated nodes miss broadcasts and fail `read` checks.

### The Architecture Decision: Anti-Entropy vs. RPC Retries

Two approaches to guarantee delivery under partitions:

1. **Message-centric (RPC retries)**: For every broadcast, send an RPC and wait for acknowledgment. Retry on timeout.
   - *Drawback*: During prolonged partitions, spawns thousands of sleeping/retrying goroutines; potential memory leaks.

2. **State-centric (anti-entropy)**: A single background worker periodically synchronizes state with neighbors.
   - *Advantage*: Predictable resource usage, conceptually simpler, used by DynamoDB and Cassandra.

The project chose **anti-entropy**. A background worker ticks periodically and synchronizes pending messages with neighbors. When messages fail to deliver, they are requeued for the next tick.

### How It Works

The `BroadcastHandler` and `GossipHandler` both follow the same pattern:
1. Parse the incoming message(s).
2. For each message, call `addMessage()`. If new, call `propagate()` to queue it for all neighbors (except the sender).
3. Reply immediately to the caller.

The background `StartGossipWorker` runs every 50ms:
1. For each neighbor, atomically extract all pending messages (`extractPending`).
2. If non-empty, spawn a goroutine to send the batch via `SyncRPC` (300ms timeout).
3. On failure, `requeueMessages` merges the failed batch back into the pending buffer.

This decouples the fast path (handler receives a message, stores it, queues it) from the slow path (network delivery with retries), keeping handlers responsive even during partitions.

### The CAP Theorem (Building an AP System)

This challenge exercises the **P** in CAP. The system is **AP (Available and Partition-tolerant)**:
- **Available**: Nodes continue accepting `broadcast` writes and `read` queries even when isolated.
- **Eventually Consistent**: Reads during a partition may return stale data. Once the network heals, the anti-entropy worker bridges the gap.

**Test parameters**: 5 nodes, 20 seconds, 10 req/s, `--nemesis partition`.

---

## Challenge 3d/3e: Efficient Broadcast (Spanning Tree)

### The Problem

Cluster size jumps to 25 nodes with strict performance constraints:

| Metric | Requirement |
|---|---|
| Messages per operation | < 30 |
| Median latency | < 400ms |
| Maximum latency | < 600ms |

Maelstrom introduces `--latency 100`, meaning every network hop costs exactly 100ms. The naive "shout to everyone" gossip from 3b/3c causes broadcast storms (80+ msgs/op, 21-second latencies).

### The Architectural Evolution

Three approaches were tried:

1. **Reliable Batching Queue**: Decouple foreground client requests from background network delivery. Instead of sending a network message per broadcast, buffer into a pending set. A background worker ticks periodically and sends batches. This reduces the number of network frames.

2. **Randomized Epidemic Gossip**: Forward batches to $k$ random nodes. Failed due to the mathematical limits of probability -- high $k$ causes broadcast storms, low $k$ causes messages to fade out (the Wallflower Problem).

3. **Deterministic Spanning Tree** (the winning approach): Override Maelstrom's provided grid topology with a computed tree.

### The Spanning Tree Algorithm

Based on the math behind a binary heap, generalized for any $k$-ary tree. Every node computes its position in the tree from the sorted node list, using a fan-out of $k = 10$:

- **Find parent**: $\text{Parent Index} = \lfloor (i - 1) / k \rfloor$ (root node 0 has no parent)
- **Find children**: $\text{Child Index} = (i \times k) + j$ for $j \in [1, k]$, bounded by cluster size.

For 25 nodes with fan-out 10:
```
              n0 (root)
       /    |    |    |   ...  \
     n1    n2   n3   n4  ...  n10
   / | \  / | \
 n11...  n21...
```

Maximum depth = 2 hops. The longest message journey is 4 hops (leaf → root → leaf on the other side).

```go
func (s *Server) TopologyHandler(m maelstrom.Message) error {
    allNodes := s.Node.NodeIDs()
    myID := s.Node.ID()
    var myIndex int
    for i, id := range allNodes {
        if id == myID {
            myIndex = i
            break
        }
    }

    s.neighbors = []string{}
    fanOut := 10

    if myIndex > 0 {
        parentIndex := (myIndex - 1) / fanOut
        s.neighbors = append(s.neighbors, allNodes[parentIndex])
    }

    for j := 1; j <= fanOut; j++ {
        childIndex := (myIndex * fanOut) + j
        if childIndex < len(allNodes) {
            s.neighbors = append(s.neighbors, allNodes[childIndex])
        }
    }

    return s.Node.Reply(m, map[string]any{"type": "topology_ok"})
}
```

Every node computes the exact same tree independently, with zero network coordination.

### How Routing Works on the Tree

When a message arrives at a node, `propagate()` forwards it to all neighbors **except** the sender:
- If a parent sends a message down → the node forwards to all children.
- If a child sends a message up → the node forwards to its parent and its *other* children.

This guarantees every node sees the message exactly once (zero duplicates), giving $O(N)$ total messages.

### The Batching + Anti-Entropy Worker

The gossip worker ticks every 50ms, extracting batched pending messages per neighbor and sending them as a single `gossip` RPC. This means:
- Multiple broadcasts arriving within a 50ms window are coalesced into one network message.
- Failed deliveries are requeued and retried on the next tick (effectively up to 20 retries/second per neighbor).
- Each `gossip()` call runs in its own goroutine with a 300ms timeout, so slow neighbors don't block others.

```go
func (s *Server) StartGossipWorker() {
    ticker := time.NewTicker(50 * time.Millisecond)
    for range ticker.C {
        for _, neighborID := range s.neighbors {
            batch := s.extractPending(neighborID)
            if len(batch) == 0 {
                continue
            }
            go s.gossip(neighborID, batch)
        }
    }
}

func (s *Server) gossip(neighborId string, messages []int64) {
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
```

### The SPOF Trade-off

While the spanning tree achieves perfect efficiency ($O(N)$ messages, $O(\log_k N)$ hops), it introduces a **Single Point of Failure (SPOF)**. Node 0 (`n0`) is the root. If it crashes or the network partitions around it, the tree severs and branches can no longer sync. This trade-off is acceptable for 3d/3e (stable network), but not for 3c (partitions), where epidemic gossip is required.

### Latest Test Results (Challenge 3d/3e)

```
:valid?        true
:count         1851 operations (894 broadcast, 957 read)
:ok-count      1851 (0 failures)
:availability  1.0 (100%)

:net
  :msgs-per-op     9.80   (target: < 30)  ✓
  :server-msgs-per-op  7.75

:stable-latencies
  p0    =   0ms
  p50   = 333ms   (target: < 400ms)  ✓
  p95   = 455ms
  p99   = 483ms
  p100  = 533ms   (target: < 600ms)  ✓

:lost-count        0
:duplicated-count  0
:never-read-count  0
```

All constraints passed with significant headroom: 9.80 msgs/op (vs 30 limit), 333ms median latency (vs 400ms limit), 533ms max latency (vs 600ms limit).

---

## Complete Message Flow

### Broadcast Path (Happy Case)

```
Client                Node A             Worker A            Node B           Node C
  │                     │                    │                  │                │
  │─ broadcast(42) ────>│                    │                  │                │
  │                     │ addMessage(42)     │                  │                │
  │                     │ → true (new)       │                  │                │
  │                     │ propagate(42,      │                  │                │
  │                     │   exclude=client)  │                  │                │
  │                     │ → pendingB += {42} │                  │                │
  │                     │ → pendingC += {42} │                  │                │
  │<─ broadcast_ok ─────│                    │                  │                │
  │                     │                    │                  │                │
  │                     │            [tick]  │                  │                │
  │                     │                    │ extractPending(B)│                │
  │                     │                    │ → [42]           │                │
  │                     │                    │─── gossip([42]) ─>│               │
  │                     │                    │                  │ addMessage(42) │
  │                     │                    │                  │ → true         │
  │                     │                    │                  │ propagate(42,  │
  │                     │                    │                  │   exclude=A)   │
  │                     │                    │<── gossip_ok ────│                │
  │                     │                    │                  │                │
  │                     │                    │ extractPending(C)│                │
  │                     │                    │ → [42]           │                │
  │                     │                    │─── gossip([42]) ─────────────────>│
  │                     │                    │                  │                │ ...
```

### Failure and Retry Path

```
Worker A             Node B (partitioned)
  │                       │
  │── gossip([42]) ──X    │  (network partition)
  │                       │
  │ [300ms timeout]       │
  │ requeueMessages(B, [42])
  │ → pendingB += {42}   │
  │                       │
  │ [next tick, 50ms]     │
  │── gossip([42]) ──X    │  (still partitioned)
  │                       │
  │ ... repeats until ... │
  │                       │
  │── gossip([42]) ──────>│  (partition heals)
  │<── gossip_ok ─────────│
```

---

## Concurrency Model

The Maelstrom Go library dispatches each message to a new goroutine. This has direct implications:

1. **All shared state must be thread-safe.** The `Server.counter` uses `atomic.Int64` (lock-free). The broadcast `messages` and `pendingMessages` maps are protected by `sync.RWMutex`.
2. **STDOUT writes are serialized** by a mutex in `Node.Send()`, preventing interleaved JSON output.
3. **STDIN reads are sequential** on the main goroutine (single scanner loop), so message ordering is preserved at the input level.
4. **The anti-entropy worker** runs in a dedicated goroutine, spawning per-neighbor child goroutines for parallel sends.

### Goroutine Topology

```
main goroutine
  │
  ├── Node.Run()           ← STDIN read loop (sequential)
  │     ├── goroutine: EchoHandler(msg1)
  │     ├── goroutine: BroadcastHandler(msg2)
  │     ├── goroutine: GossipHandler(msg3)
  │     └── ...            ← one goroutine per incoming message
  │
  └── StartGossipWorker()  ← ticker loop (50ms)
        ├── goroutine: gossip("n1", [42, 43])
        ├── goroutine: gossip("n2", [44])
        └── ...            ← one goroutine per neighbor per tick
```

### Lock Hierarchy

| Lock | Protects | Acquired by |
|---|---|---|
| `broadcastMu` (RWMutex) | `messages` map | `addMessage` (W), `getBroadcastMessages` (R) |
| `pendingMu` (RWMutex) | `pendingMessages` map | `addPendingMessage` (W), `extractPending` (W), `requeueMessages` (W) |
| `counter` (atomic) | Unique ID sequence | `UniqueIdHandler` (lock-free) |
| `Node` internal mutex | STDOUT | `Reply`, `Send`, `SyncRPC` |

The two RWMutexes are independent (no nested locking), so deadlocks between them are impossible.

---

## Distributed Systems Concepts Demonstrated

| Concept | Where It Appears |
|---|---|
| Coordination-free design | Unique ID generation needs no inter-node communication |
| CAP theorem (AP) | Nodes remain available during partitions, sacrificing strong consistency |
| Gossip / flooding protocol | Challenges 3b/3c broadcast to neighbors |
| Spanning tree topology | Challenges 3d/3e compute a deterministic k-ary tree for efficient routing |
| Anti-entropy synchronization | Background worker periodically reconciles state (inspired by DynamoDB/Cassandra) |
| Idempotency / deduplication | `addMessage` prevents reprocessing and infinite loops |
| Batching | Pending messages accumulated between ticks are sent as a single RPC |
| Retry with timeout | `gossip()` uses 300ms context timeout, `requeueMessages` on failure |
| CRDTs (grow-only set) | The broadcast store is a G-Set: only additions, merge = union |
| Fire-and-forget vs. acknowledged delivery | Evolution from `Send()` (3b, lossy) to `SyncRPC()` (3d, acknowledged) |
| TOCTOU race condition | Fixed by combining check-and-write into a single locked operation |
| Atomic swap (extract-and-clear) | `extractPending` replaces the bucket in O(1) to avoid races |

---

## Build System

| Target | What It Does |
|---|---|
| `build` | `go build -o bin/node ./cmd/node` |
| `challenge-1` | Echo test: 1 node, 10s |
| `challenge-2` | Unique IDs test: 3 nodes, 30s, 1000 req/s, partitions |
| `challenge-3a` | Single-node broadcast: 1 node, 20s, 10 req/s |
| `challenge-3b` | Multi-node broadcast: 5 nodes, 20s, 10 req/s |
| `challenge-3c` | Fault-tolerant broadcast: 5 nodes, 20s, 10 req/s, partitions |
| `challenge-3d` | Efficient broadcast: 25 nodes, 20s, 100 req/s, 100ms latency |
| `challenge-3e` | Efficient broadcast part 2: same parameters as 3d |
| `clean` | Removes `bin/` and `store/` |

Maelstrom test flags:
- `-w <workload>`: which workload to run (echo, unique-ids, broadcast, etc.)
- `--bin <path>`: path to the node binary
- `--node-count N`: how many node processes to spawn
- `--time-limit N`: test duration in seconds
- `--rate N`: target requests per second
- `--availability total`: every request must succeed (even during partitions)
- `--nemesis partition`: inject network partitions during the test
- `--latency N`: simulated network latency per hop (ms)

---

## Test Results (Maelstrom Output)

Maelstrom writes results to `store/<workload>/<timestamp>/`, producing:

- `results.edn` - Structured pass/fail summary (Clojure EDN format)
- `jepsen.log` - Detailed test execution log
- `history.txt` / `history.edn` - Full operation history
- `timeline.html` - Interactive timeline visualization
- `messages.svg` - Inter-node message flow diagram
- `latency-raw.png`, `latency-quantiles.png` - Latency distributions
- `rate.png` - Throughput over time
- `node-logs/` - STDERR output from each node

Maelstrom checks multiple properties:
- **Availability**: fraction of operations that succeeded (target: 1.0)
- **Workload-specific**: e.g., no duplicate IDs, all echoes correct, no lost broadcasts
- **Performance**: latency and rate graphs generated successfully
- **Network**: message counts and per-operation message overhead
- **Exceptions**: no unhandled crashes

### Results Summary

| Challenge | Nodes | Ops | Failures | Msgs/Op | Median Latency | Max Latency | Notes |
|---|---|---|---|---|---|---|---|
| 1 (Echo) | 1 | 47 | 0 | 2.04 | - | - | Trivial pass |
| 2 (Unique IDs) | 3 | 24,650 | 0 | 2.00 | - | - | 0 duplicates, under partitions |
| 3d/3e (Broadcast) | 25 | 1,851 | 0 | 9.80 | 333ms | 533ms | 0 lost, 0 duplicated |

---

## Archived Implementations (Historical Evolution)

The `archived/` directory preserves earlier standalone implementations. Each was its own Go module. Studying them reveals the learning progression:

### Challenge 1 (archived): Echo

Identical logic to the current version, but as a flat `main.go` with an inline closure handler instead of a method on a struct.

### Challenge 2 (archived): Unique IDs - Experimentation

The archived version reveals experimentation with multiple ID generation strategies:

1. **Google UUID (`uuid.New().String()`)** - Passed. Relies on cryptographic randomness. Works but is heavier than needed.
2. **MongoDB ObjectID (`primitive.NewObjectID().Hex()`)** - Passed. 12-byte IDs combining timestamp + machine ID + process ID + counter. The final chosen approach in the archive.
3. **Twitter Snowflake (`snowflake.Node.Generate()`)** - **Did not work**. Required passing the Node ID as an integer, but Maelstrom assigns string IDs like `"n0"`.
4. **Timestamp only (`time.Now().UnixMicro()`)** - **Did not work**. Collisions between nodes generating the same microsecond timestamp.

The current implementation supersedes all of these with the simpler composite key approach (zero external dependencies).

### Challenge 3a (archived): Single-Node Broadcast

A naive single-node implementation:
- Stores broadcast messages in a plain `[]float64` slice (not thread-safe).
- No inter-node communication, no mutex protection.
- Only works for single-node tests.

### Challenge 3b (archived): Multi-Node Broadcast

Adds multi-node support:
1. **Thread-safe storage**: switched from `[]float64` to `map[int]struct{}` protected by `sync.RWMutex`.
2. **Deduplication**: checks if a message was already seen before processing.
3. **Flooding protocol**: on receiving a new broadcast, forwards to every other node (excluding self and sender) using `Node.Send()` (fire-and-forget).
4. **Peer vs. client routing hack**: replies `broadcast_ok` only to Maelstrom clients (IDs starting with `c`), not to peer nodes (IDs starting with `n`).

Weakness: `Node.Send()` is fire-and-forget. If a message is dropped, it's lost permanently.

### Challenge 3c (archived): Fault-Tolerant Broadcast

The most sophisticated archived implementation:
1. **`MemStore`**: A dedicated thread-safe KV store, cleanly separated into its own file.
2. **RPC with retries**: Uses `Node.RPC()` with callbacks. An `ack` flag (mutex-protected) tracks delivery.
3. **Goroutine-per-peer**: Each broadcast spawns a goroutine per target that retries up to 100 times.
4. **Linear backoff**: `i * 100ms` on the i-th retry (100ms, 200ms, 300ms...).
5. **Non-blocking**: Reply to client immediately; retries are asynchronous background work.

### Evolution from Archived to Current

The current implementation represents a significant architectural shift from the archived code:

| Aspect | Archived 3c | Current 3d/3e |
|---|---|---|
| Delivery strategy | Per-message RPC retries | State-centric anti-entropy worker |
| Retry mechanism | Goroutine-per-peer with linear backoff | Ticker-based batch requeue (50ms) |
| Topology | Flood to all nodes | Deterministic spanning tree (fan-out 10) |
| Message format | Individual broadcasts | Batched gossip arrays |
| RPC style | Async `Node.RPC()` + callback | Sync `Node.SyncRPC()` + timeout |
| Scalability | Poor (goroutine explosion under partitions) | Good (bounded goroutines, batching) |

---

## Design Decisions and Observations

### Single Binary, Multiple Handlers
All challenge handlers coexist in one binary. Maelstrom only sends the relevant message types per workload, so unused handlers are inert. This simplifies the build and avoids code duplication.

### No External Dependencies in Current Code
The refactored version dropped UUID, Snowflake, and MongoDB libraries in favor of a zero-dependency composite key. Simpler and more educational.

### Atomic vs. Mutex
The unique ID handler uses `atomic.Int64` instead of a mutex-protected counter. Atomic operations are lock-free and significantly faster for simple increment operations.

### The `json.Number` Choice
The `BroadcastHandler` uses `json.Number` instead of `float64` for message values. This avoids floating-point precision loss when converting JSON numbers to integers (Go's `json.Unmarshal` defaults to `float64` for numbers in `interface{}` values).

### Topology Computed Locally, Not from Maelstrom
The `TopologyHandler` ignores the topology Maelstrom sends and computes its own spanning tree. This is allowed and preferred because the default topology (grid) is suboptimal for the efficiency constraints.

### `neighbors` Without a Mutex
The `neighbors` slice has no mutex protection. This is safe because it is written exactly once (when the `topology` message arrives at startup) and only read afterwards. The Go memory model guarantees visibility via happens-before from the Maelstrom message dispatch.

---

## Potential Improvements

- **Challenge 3c regression**: The current spanning tree topology introduces a SPOF that would fail challenge 3c (partitions). A hybrid approach (spanning tree for efficiency + fallback epidemic gossip during detected failures) could pass both 3c and 3d/3e.
- **No Go unit tests**: The project relies entirely on Maelstrom integration tests. Unit tests for `addMessage`, `extractPending`, `propagate`, etc. would be straightforward and catch regressions faster.
- **Dynamic fan-out**: The fan-out is hardcoded to 10. Computing an optimal fan-out based on cluster size and latency constraints would make the implementation more general.
- **Adaptive tick interval**: The 50ms tick is a fixed trade-off between latency and network overhead. An adaptive interval (faster during high throughput, slower during quiet periods) could improve both.
- **Gossip protocol metrics**: Adding counters for messages sent/received, retries, and batch sizes would aid debugging and performance tuning.

---

## Available Maelstrom Workloads (Not Yet Attempted)

Based on the Maelstrom documentation, the following workloads remain:

| Workload | Description |
|---|---|
| G-Counter | Grow-only counter (CRDT) |
| G-Set | Grow-only set (CRDT) |
| Pn-Counter | Positive-negative counter (increment + decrement) |
| Kafka | Append-only log with offsets, polls, and committed offsets |
| Lin-kv | Linearizable key-value store |
| Txn-list-append | Transactional list-append workload |
| Txn-rw-register | Transactional read-write register workload |

Maelstrom also provides built-in services (`lin-kv`, `seq-kv`, `lww-kv`, `lin-tso`) that nodes can use as building blocks for more complex systems -- for example, using the linearizable KV store as a foundation for a transaction protocol.

---

## Git History

The project evolved in two phases:

1. **Phase 1**: Individual challenge solutions, each as a standalone Go module. Progressed from echo through fault-tolerant broadcast (challenges 1, 2, 3a, 3b, 3c).
2. **Phase 2**: Archived all old implementations, restructured into `cmd/internal` layout, re-implemented all challenges (1, 2, 3a-3e) in the unified binary with cleaner code, spanning tree topology, and batched anti-entropy.
