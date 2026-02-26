# Gossip Glomers — Deep Research Report

## 1. What This Project Is

This repository is a Go implementation of the [Gossip Glomers](https://fly.io/dist-sys/) distributed systems challenge series, created by Fly.io in collaboration with Kyle Kingsbury (Jepsen). The challenges use **Maelstrom** — a workbench built on top of the Jepsen testing framework — to simulate a cluster of nodes communicating over a JSON-based protocol via STDIN/STDOUT. Maelstrom acts as the network layer: it routes messages, injects faults (network partitions), and validates correctness.

- **Author:** Tira Sundara
- **Module:** `github.com/tirasundaraa/gossip-glomers`
- **Go version:** 1.24.2 (current rewrite), originally 1.19 (archived)
- **Key dependency:** `github.com/jepsen-io/maelstrom/demo/go` — the official Maelstrom Go client library

---

## 2. Two Eras of This Repository

The git history reveals two distinct phases:

### Phase 1: Original Implementation (April 2023)

Nine commits across two days (`6ff6303` through `f4cca09`) produced five standalone challenges, each as its own Go module under a flat directory structure. These are now preserved in `archived/`.

### Phase 2: Unified Rewrite (Current — February 2026)

The latest commit (`aab8142`) archived the original code and started fresh with a single Go module, a proper `cmd/internal` layout, and a Makefile-driven build. Only Challenge 1 (Echo) has been re-implemented so far in this new structure.

The rewrite signals an intent to tackle the challenges again with cleaner architecture and shared infrastructure.

---

## 3. Repository Structure

```
gossip-glomers/
├── cmd/
│   └── node/
│       └── main.go              # Single entry point for all challenges
├── internal/
│   └── node/
│       ├── node.go              # Server struct wrapping maelstrom.Node
│       └── echo.go              # Echo handler (Challenge 1)
├── archived/                    # Original per-challenge implementations
│   ├── research.md              # Previous research report
│   ├── challenge-1-echo/
│   ├── challenge-2-unique-ids/
│   ├── challenge-3a-broadcast/
│   ├── challenge-3b-broadcast/
│   └── challenge-3c-broadcast/
├── maelstrom/                   # Maelstrom binary + documentation
│   ├── maelstrom               # The test harness executable
│   ├── doc/
│   │   ├── protocol.md          # Wire protocol spec
│   │   ├── workloads.md         # All workload RPC definitions
│   │   ├── services.md          # Maelstrom-provided KV stores, TSO, etc.
│   │   ├── 03-broadcast/        # Broadcast challenge walkthrough
│   │   └── ...
│   └── demo/                    # Reference implementations (Ruby, Java, Go, Clojure)
├── store/                       # Maelstrom test output (results, logs, SVGs)
├── Makefile
├── go.mod
├── go.sum
├── .gitignore
└── README.md
```

---

## 4. The Maelstrom Protocol

Understanding Maelstrom is essential to understanding this project.

### 4.1 Communication Model

Nodes are OS processes. They read JSON messages (one per line) from STDIN and write JSON messages to STDOUT. STDERR is captured as debug logs. Maelstrom sits between all nodes, acting as the network — it can delay, reorder, or drop messages to simulate real network conditions.

### 4.2 Message Format

Every message has three fields:

```json
{
  "src":  "n1",
  "dest": "n2",
  "body": { "type": "echo", "msg_id": 1, "echo": "hello" }
}
```

The `body` always contains a `type` field. Request-response pairs are correlated via `msg_id` and `in_reply_to`.

### 4.3 Node Identity

- Server nodes: `n0`, `n1`, `n2`, ... (instances of your binary)
- Client nodes: `c0`, `c1`, `c2`, ... (Maelstrom's internal workload generators)

### 4.4 Initialization

Before any workload traffic, Maelstrom sends an `init` message to each node with its `node_id` and the full list of `node_ids` in the cluster. The node must reply `init_ok`. The Go library (`maelstrom.NewNode()`) handles this automatically.

### 4.5 Error Codes

Maelstrom defines error codes 0–30 (timeout, not-supported, temporarily-unavailable, malformed-request, crash, abort, key-does-not-exist, key-already-exists, precondition-failed, txn-conflict). Errors are classified as **definite** (operation definitely didn't happen) or **indefinite** (might have happened). Custom codes 1000+ are always indefinite.

### 4.6 Available Services

Maelstrom provides built-in services that nodes can use as building blocks:

| Service | Consistency | Description |
|---------|-------------|-------------|
| `lin-kv` | Linearizable | Read/write/CAS key-value store |
| `seq-kv` | Sequential | Relaxed: operations appear in a total order |
| `lww-kv` | Last-write-wins | Intentionally pathological eventual consistency |
| `lin-tso` | Linearizable | Monotonically increasing timestamp oracle |

These become relevant for later challenges (counters, Kafka, transactions).

---

## 5. The Go Client Library

The `github.com/jepsen-io/maelstrom/demo/go` package provides:

| API | Purpose |
|-----|---------|
| `maelstrom.NewNode()` | Creates a node, handles `init` automatically |
| `n.Handle(type, fn)` | Registers a handler for a message type |
| `n.Run()` | Starts the STDIN read loop and dispatches messages |
| `n.Reply(msg, body)` | Sends a response (sets `in_reply_to` automatically) |
| `n.Send(dest, body)` | Fire-and-forget message send |
| `n.RPC(dest, body, handler)` | Send + register a callback for the response |
| `n.ID()` | Returns this node's string ID (e.g., `"n0"`) |
| `n.NodeIDs()` | Returns all node IDs in the cluster |

The `Message` struct contains `Src`, `Dest`, and `Body` (raw `json.RawMessage`). Handlers have the signature `func(maelstrom.Message) error`.

---

## 6. Current Architecture (Rewrite)

### 6.1 Server Abstraction

```go
// internal/node/node.go
type Server struct {
    Node *maelstrom.Node
}

func NewServer() *Server {
    return &Server{Node: maelstrom.NewNode()}
}
```

The `Server` struct wraps the Maelstrom node and will accumulate state and handlers as more challenges are implemented. Handlers are methods on `Server`, keeping state co-located with behavior.

### 6.2 Entry Point

```go
// cmd/node/main.go
func main() {
    s := node.NewServer()
    s.Node.Handle("echo", s.EchoHandler)
    if err := s.Node.Run(); err != nil {
        log.Fatal(err)
    }
}
```

A single binary serves all challenges. New handlers will be registered here as they're implemented.

### 6.3 Build System

The Makefile provides:

| Target | Command |
|--------|---------|
| `build` | `go build -o ./bin/node ./cmd/node` |
| `challenge-1` | Builds, then runs `maelstrom test -w echo --bin ./bin/node --node-count 1 --time-limit 10` |
| `clean` | Removes `bin/` and `store/` |

This is a significant improvement over the archived code, which had no Makefile and relied on per-challenge `test.sh` scripts that depended on a `$MAELSTROM_PATH` environment variable.

---

## 7. Challenge Deep Dives

### 7.1 Challenge 1: Echo (Implemented)

**Workload:** `echo` — clients send a payload, expect the same payload back.

**Implementation (current):**

```go
// internal/node/echo.go
func (s *Server) EchoHandler(m maelstrom.Message) error {
    var body map[string]any
    if err := json.Unmarshal(m.Body, &body); err != nil {
        return err
    }
    body["type"] = "echo_ok"
    return s.Node.Reply(m, body)
}
```

- Completely stateless
- Uses `map[string]any` for message bodies — a pattern consistent across all challenges
- Unmarshal, mutate the `type` field, reply. Nothing else.

**Test results** (from `store/echo/`): 47 operations, 100% success, 0 failures. ~2 messages per operation (one request, one response). Verified passing.

### 7.2 Challenge 2: Unique ID Generation (Archived)

**Workload:** `unique-ids` — 3 nodes, 1000 req/s, network partitions, total availability required.

**The exploration journey** (preserved in code comments):

| Strategy | Library | Result | Problem |
|----------|---------|--------|---------|
| UUID v4 | `github.com/google/uuid` | PASS | None — pure randomness needs no coordination |
| MongoDB ObjectID | `go.mongodb.org/mongo-driver` | PASS (active) | None — embeds timestamp + random + counter |
| Snowflake | `github.com/bwmarrin/snowflake` | FAIL | `NewNode()` requires `int64`, but `n.ID()` returns `"n0"` |
| Unix microsecond | `time.Now().UnixMicro()` | FAIL | Collisions across concurrent nodes |

**Active solution:** `primitive.NewObjectID().Hex()` — a 24-character hex string from a 12-byte value (4-byte timestamp, 5-byte random, 3-byte incrementing counter). Coordination-free and globally unique.

**Key insight:** The challenge deliberately requires partition tolerance + total availability, forcing solutions that work without any inter-node communication. This is a CAP theorem lesson: uniqueness here is a property that doesn't require consensus.

### 7.3 Challenge 3a: Single-Node Broadcast (Archived)

**Workload:** `broadcast` — 1 node (effectively), 10 req/s, no faults.

**State:** A bare `[]float64` slice on a `server` struct.

**Handlers:**
- `broadcast`: Append `body["message"].(float64)` to slice, reply `broadcast_ok`
- `read`: Return the full slice as `messages`
- `topology`: Acknowledge and ignore

**Problems:**
- Not thread-safe — `ids` slice mutated without synchronization (Maelstrom handlers can run concurrently)
- No inter-node communication — only works because the test effectively uses one node
- Uses `float64` for message values due to Go's JSON `any` unmarshaling behavior

### 7.4 Challenge 3b: Multi-Node Broadcast (Archived)

**Workload:** `broadcast` — 5 nodes, 10 req/s, no faults.

**Key changes from 3a:**

1. **Thread-safe set:** `map[int]struct{}` with `sync.RWMutex` (idiomatic Go set — `struct{}` is zero-size)
2. **Deduplication:** Check-before-store prevents infinite broadcast loops
3. **Flooding gossip:** Forward new messages to all nodes except self and sender

**Algorithm:**
```
receive broadcast(msg):
  lock → if seen, unlock and drop silently
       → else store, unlock
  for each peer (excluding self and sender):
    n.Send(peer, body)       // fire-and-forget
  reply broadcast_ok
```

**Notable design choices:**
- Manual lock/unlock (no `defer`) so the lock is released before I/O
- `n.Send()` is fire-and-forget — no delivery guarantee
- Duplicate broadcasts from peers get `nil` return (no reply) — works because `Send` doesn't expect responses
- No fault tolerance — a single dropped message is permanently lost

### 7.5 Challenge 3c: Fault-Tolerant Broadcast (Archived)

**Workload:** `broadcast` — 5 nodes, 10 req/s, **network partitions**.

This is the most sophisticated implementation in the repository.

**New: `MemStore` abstraction** (`mem_store.go`):

```go
type MemStore struct {
    mu sync.RWMutex
    m  map[int]struct{}
}
```

Methods: `Put(k, v)`, `IsExist(k)`, `Keys()`. All use `defer` for unlock (safer but holds lock slightly longer than 3b's manual approach). `Put` returns `error` (always nil) — suggests planned extensibility.

**Retry mechanism:**

```
receive broadcast(msg):
  if store.IsExist(id) → drop
  store.Put(id)
  for each peer (excluding self and sender):
    spawn goroutine:
      ack = false
      for i in 1..100 while !ack:
        n.RPC(peer, body, func() { ack = true })
        sleep(i * 100ms)      // linear backoff
  reply broadcast_ok immediately
```

**Critical differences from 3b:**

| Aspect | 3b | 3c |
|--------|----|----|
| Send primitive | `n.Send()` (fire-and-forget) | `n.RPC()` (request + callback) |
| Acknowledgments | None | Callback sets `ack = true` |
| Retries | None | Up to 100 attempts |
| Backoff | N/A | Linear: `i * 100ms` (100ms, 200ms, ..., 10s) |
| Concurrency | Sequential sends | Goroutine per destination |
| State abstraction | Inline map + mutex | Extracted `MemStore` |

**Concurrency model:**
- Each peer gets a dedicated goroutine for retries
- `ack` flag + `ackMu` mutex are goroutine-local (one pair per peer closure)
- The handler returns `broadcast_ok` immediately — doesn't block on replication
- This is **async replication**: client gets confirmation before all peers acknowledge

**Subtle race condition:** The `for` loop reads `ack` without holding `ackMu` in its condition check. The mutex only protects the write inside `ackHandler`. This is technically a data race detectable by `go test -race`, but it's benign — worst case is one extra unnecessary RPC call.

**Backoff analysis:** Linear backoff `i * 100ms` produces delays: 100ms, 200ms, ..., 10000ms. Maximum total wait: ~500 seconds (sum 1..100 * 100ms). In practice, ack arrives quickly once the partition heals. Exponential backoff would be more standard (reduces load during prolonged partitions) but linear works fine for this test duration.

**Delivery semantics:** At-least-once delivery (retries ensure delivery) combined with idempotent handlers (deduplication via set) achieves effectively-once processing.

---

## 8. Distributed Systems Concepts Demonstrated

### 8.1 Flooding Protocol
Challenges 3b and 3c implement **epidemic/flooding broadcast**: every node forwards every new message to every other node. Message complexity is O(n²) per broadcast, but the algorithm is simple and correct. The Maelstrom docs show that tree topologies achieve optimal O(n) messages per broadcast with O(log n) latency.

### 8.2 Deduplication and Idempotency
The `map[int]struct{}` set prevents infinite message loops. Without it, A sends to B, B sends back to A, infinitely. The deduplication also makes handlers idempotent — processing the same message twice has no additional effect.

### 8.3 Coordination-Free Unique IDs
Challenge 2 proves that globally unique IDs can be generated without consensus. UUID v4 uses 122 bits of randomness (collision probability ~1 in 2^61 for a billion IDs). MongoDB ObjectID embeds timestamp + machine randomness + counter, providing structural uniqueness without coordination.

### 8.4 Network Partition Tolerance
Challenge 3c's retry loop with backoff ensures messages buffered during a partition are eventually delivered when connectivity restores. This is a practical implementation of partition tolerance from the CAP theorem.

### 8.5 At-Least-Once vs Exactly-Once
- 3b provides **at-most-once** delivery (fire-and-forget, no retry)
- 3c provides **at-least-once** delivery (retries until ack)
- Combined with idempotent handlers, 3c achieves **effectively-once** semantics

### 8.6 Topology Ignored
All broadcast challenges receive a `topology` message describing the network graph but never use it. The flooding approach sends to *all* nodes regardless. Using the provided topology (e.g., a spanning tree) would reduce message complexity from O(n²) to O(n) — a clear optimization left on the table.

---

## 9. The Maelstrom Workloads (Full Challenge List)

The repository only implements 3 of 6+ challenge categories. Here's the full scope:

| # | Challenge | Workload | Key Concept | Status |
|---|-----------|----------|-------------|--------|
| 1 | Echo | `echo` | Protocol basics | Done (rewritten) |
| 2 | Unique IDs | `unique-ids` | Coordination-free generation | Done (archived) |
| 3a | Single-Node Broadcast | `broadcast` | State management | Done (archived) |
| 3b | Multi-Node Broadcast | `broadcast` | Gossip / flooding | Done (archived) |
| 3c | Fault-Tolerant Broadcast | `broadcast` | Retries, partition tolerance | Done (archived) |
| 3d | Efficient Broadcast I | `broadcast` | Message efficiency targets | Not started |
| 3e | Efficient Broadcast II | `broadcast` | Latency targets | Not started |
| 4 | Grow-Only Counter | `g-counter` | CRDTs, `seq-kv` service | Not started |
| 5a | Single-Node Kafka | `kafka` | Log-structured storage | Not started |
| 5b | Multi-Node Kafka | `kafka` | Distributed logs | Not started |
| 5c | Efficient Kafka | `kafka` | Optimization | Not started |
| 6a | Single-Node Transactions | `txn-rw-register` | Serializability | Not started |
| 6b | Totally-Available Transactions | `txn-rw-register` | Read uncommitted | Not started |
| 6c | Serializable Transactions | `txn-rw-register` | Consistency models | Not started |

The unstarted challenges introduce increasingly sophisticated distributed systems concepts: CRDTs for conflict-free replicated counters, Kafka-style append-only logs with offset management, and transactional workloads requiring different consistency models (read uncommitted through serializable).

---

## 10. Test Infrastructure Details

### 10.1 Maelstrom Test Configuration

| Challenge | Workload | Nodes | Time Limit | Rate | Nemesis | Availability |
|-----------|----------|-------|------------|------|---------|--------------|
| 1 | `echo` | 1 | 10s | 5/s (default) | none | — |
| 2 | `unique-ids` | 3 | 30s | 1000/s | partition | total |
| 3a | `broadcast` | 1 | 20s | 10/s | none | — |
| 3b | `broadcast` | 5 | 20s | 10/s | none | — |
| 3c | `broadcast` | 5 | 20s | 10/s | partition | — |

### 10.2 Test Output

Maelstrom writes results to `store/<workload>/<timestamp>/`:
- `results.edn` — structured test results (validity, stats, network metrics)
- `jepsen.log` — full Jepsen test runner log
- `node-logs/n0.log` — per-node STDERR output
- `history.txt` / `history.edn` — operation history
- `messages.svg` — Lamport diagram of all messages
- `timeline.html` — interactive timeline visualization

### 10.3 Most Recent Test Run

The `store/echo/` directory contains a successful run from 2026-02-26:
- 47 echo operations, all successful
- 96 total messages (2 per operation: request + response)
- 100% availability
- 0 workload errors

---

## 11. Code Patterns and Idioms

### 11.1 Loosely-Typed Messages
Every challenge uses `map[string]any` rather than typed structs. This means:
- No compile-time safety on message fields
- Type assertions required: `body["message"].(float64)`
- Flexibility to add/remove fields without changing types
- Go's `encoding/json` unmarshals numbers as `float64` when target is `any`

### 11.2 Handler Registration
```go
n.Handle("type", handlerFunc)  // register
n.Run()                         // event loop (blocks)
```
This is an event-driven architecture. The node's `Run()` reads STDIN line by line, parses JSON, dispatches to handlers by `type`, and handlers reply via STDOUT.

### 11.3 Server Struct as Handler Container
The current rewrite and archived 3a/3b/3c all use a struct to group the node with its state, and handlers as methods on that struct. This avoids globals and makes state ownership explicit.

### 11.4 Zero-Size Set Pattern
`map[int]struct{}` is idiomatic Go for a set — `struct{}` occupies zero bytes, so only keys consume memory.

---

## 12. Known Issues and Technical Debt

1. **Challenge 3a data race:** `[]float64` slice accessed without synchronization. Works by luck with low concurrency.

2. **Challenge 3c benign data race:** `ack` read in `for` loop condition without holding `ackMu`. Detectable by `-race` flag but harmless in practice.

3. **Linear vs exponential backoff in 3c:** `i * 100ms` grows linearly. Exponential (`100ms * 2^i` with a cap) would reduce load during prolonged partitions.

4. **Topology ignored everywhere:** All broadcast challenges use full-mesh flooding. The Maelstrom docs demonstrate that spanning trees achieve optimal O(n) messages with O(log n) latency.

5. **No reply on duplicate broadcasts in 3b:** When a node receives a duplicate inter-node broadcast, it returns `nil` (no reply). This is fine for `Send`-based inter-node traffic but would break if Maelstrom clients sent duplicate broadcasts.

6. **`MemStore.Put` returns unused error:** Suggests planned extensibility that was never realized.

7. **Archived code has compiled binaries in git:** Platform-specific binaries were committed. The new `.gitignore` correctly excludes `bin/`.

---

## 13. Evolution from Archived to Current

| Aspect | Archived | Current Rewrite |
|--------|----------|-----------------|
| Module structure | One Go module per challenge | Single unified module |
| Entry point | `main.go` per challenge | `cmd/node/main.go` |
| Build | Manual `go build` + `test.sh` | Makefile with targets |
| Shared code | None — each challenge is independent | `internal/node` package |
| State management | Inline in handlers or `server` struct | `Server` struct in dedicated file |
| Maelstrom path | `$MAELSTROM_PATH` env var | `./maelstrom/maelstrom` (vendored) |
| Go version | 1.19 | 1.24.2 |
| Maelstrom lib version | `20230410` / `20230412` | `20251128` |
| GitHub module path | `github.com/tirasundara/...` | `github.com/tirasundaraa/gossip-glomers` |

The rewrite brings the Maelstrom binary directly into the repo (in `maelstrom/`), eliminating the external dependency on `$MAELSTROM_PATH`. The unified module structure means future challenges can share code through `internal/` packages.

---

## 14. What's Next

The project is positioned to continue with Challenge 2 (Unique IDs) in the new architecture, followed by the broadcast series (3a–3e). The remaining challenges introduce progressively harder distributed systems problems:

- **Challenge 4 (G-Counter):** Requires using Maelstrom's `seq-kv` service to build a grow-only counter. Introduces CRDTs or service-backed state.
- **Challenge 5 (Kafka):** Log-structured storage with offsets, committed offsets, and multi-node replication.
- **Challenge 6 (Transactions):** The hardest challenges — building a transactional KV store supporting read-uncommitted through serializable isolation.

The `Server` struct pattern in the current rewrite is well-suited for this: handlers can share state, and the `internal/node` package can grow to include storage abstractions, retry utilities, and topology-aware routing.
