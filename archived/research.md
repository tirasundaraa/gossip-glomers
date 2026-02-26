# Gossip Glomers — Project Research Report

## 1. Project Overview

This repository contains solutions to the [Gossip Glomers](https://fly.io/dist-sys/) distributed systems challenges, created by Fly.io in collaboration with Kyle Kingsbury (of Jepsen fame). The challenges use **Maelstrom**, a workbench for learning distributed systems built on top of the Jepsen testing framework. Nodes communicate over STDIN/STDOUT using a JSON-based protocol, and Maelstrom acts as the network, injecting faults and validating correctness.

- **Author:** Tira Sundara (`sundaralinus@gmail.com`)
- **Language:** Go 1.19
- **Repository:** `git@github.com:tirasundaraa/gossip-glomers.git`
- **Development period:** April 14–15, 2023 (9 commits over 2 days)
- **Challenges completed:** 5 (Echo, Unique IDs, Broadcast 3a/3b/3c)

---

## 2. Repository Structure

```
gossip-glomers/
├── README.md
├── challenge-1-echo/
│   ├── go.mod / go.sum
│   ├── main.go              (30 lines)
│   ├── maelstrom-echo        (compiled binary)
│   └── test.sh
├── challenge-2-unique-ids/
│   ├── go.mod / go.sum
│   ├── main.go              (57 lines)
│   ├── maelstrom-unique-ids   (compiled binary)
│   └── test.sh
├── challenge-3a-broadcast/
│   ├── go.mod / go.sum
│   ├── main.go              (54 lines)
│   ├── maelstrom-broadcast    (compiled binary)
│   └── test.sh
├── challenge-3b-broadcast/
│   ├── go.mod / go.sum
│   ├── main.go              (83 lines)
│   ├── maelstrom-broadcast    (compiled binary)
│   └── test.sh
└── challenge-3c-broadcast/
    ├── go.mod / go.sum
    ├── main.go              (96 lines)
    ├── mem_store.go          (43 lines)
    ├── maelstrom-broadcast    (compiled binary)
    └── test.sh
```

Each challenge is a **self-contained Go module** with its own `go.mod`, source files, test script, and pre-compiled binary. There is no shared library or workspace-level module — each directory is fully independent.

---

## 3. Build System and Testing

### Build

Standard Go compilation, one binary per challenge:

```bash
cd challenge-X/
go build -o <binary-name>
```

### Test Execution

Each challenge includes a `test.sh` that:
1. Captures the current directory (`cwd=$(pwd)`)
2. Builds the Go binary
3. Changes to `$MAELSTROM_PATH` (environment variable pointing to the Maelstrom installation)
4. Invokes `./maelstrom test` with challenge-specific flags
5. Returns to the original directory

There is no CI/CD, no Makefile, and no containerization.

### Test Configurations

| Challenge | Workload     | Nodes | Time Limit | Rate     | Nemesis    |
|-----------|-------------|-------|------------|----------|------------|
| 1         | `echo`      | 1     | 10s        | default  | none       |
| 2         | `unique-ids`| 3     | 30s        | 1000/s   | partition  |
| 3a        | `broadcast` | 5     | 20s        | 10/s     | none       |
| 3b        | `broadcast` | 5     | 20s        | 10/s     | none       |
| 3c        | `broadcast` | 5     | 20s        | 10/s     | partition  |

Key observation: Challenge 2 already uses network partitions (`--nemesis partition`) and `--availability total`, meaning the unique ID generator must produce globally unique IDs even during network splits. Challenge 3c reintroduces partitions for broadcast.

---

## 4. Dependencies

All challenges depend on the Maelstrom Go client library:
- `github.com/jepsen-io/maelstrom/demo/go`

Two versions are used:
- `v0.0.0-20230410034848-d1ba02cffac2` (challenges 1, 2, 3a)
- `v0.0.0-20230412195810-5aead5336e5f` (challenges 3b, 3c — a newer revision)

Challenge 2 additionally depends on:
- `github.com/google/uuid v1.3.0` — UUID v4 generation
- `github.com/bwmarrin/snowflake v0.3.0` — Twitter Snowflake IDs
- `go.mongodb.org/mongo-driver v1.11.4` — MongoDB ObjectID generation

---

## 5. Challenge-by-Challenge Deep Dive

### 5.1 Challenge 1: Echo

**File:** `challenge-1-echo/main.go` (30 lines)

The simplest challenge. A node receives an `"echo"` message and must reply with the same body, changing the type to `"echo_ok"`.

**How it works:**
1. Create a Maelstrom node via `maelstrom.NewNode()`
2. Register a handler for `"echo"` messages
3. Unmarshal the JSON body into `map[string]any`
4. Overwrite `body["type"]` with `"echo_ok"`
5. Reply using `n.Reply(msg, body)`
6. Run the event loop with `n.Run()`

**Design notes:**
- Completely stateless — no data is stored between requests
- The loosely-typed `map[string]any` pattern is used throughout the project to avoid defining rigid structs for every message type
- Error propagation is simple: unmarshal or reply errors are returned directly, causing Maelstrom to detect the failure

---

### 5.2 Challenge 2: Unique ID Generation

**File:** `challenge-2-unique-ids/main.go` (57 lines)

Nodes must generate globally unique IDs. The system is tested with 3 nodes, 1000 requests/second, and network partitions — so IDs must be unique without any coordination.

**How it works:**
1. Register a handler for `"generate"` messages
2. Generate a unique ID and return it in `body["id"]`
3. Reply with type `"generate_ok"`

**ID Generation Strategy Evolution (documented in comments):**

The code preserves the author's experimentation journey:

| Strategy | Library | Result | Why |
|----------|---------|--------|-----|
| UUID v4 | `github.com/google/uuid` | PASS | Randomness provides uniqueness without coordination |
| MongoDB ObjectID | `go.mongodb.org/mongo-driver` | PASS (active) | 12-byte ID with timestamp + machine + counter components |
| Snowflake | `github.com/bwmarrin/snowflake` | Failed | Requires a numeric node ID, but Maelstrom node IDs are strings like `"n0"`, `"n1"` |
| Unix Microsecond Timestamp | stdlib `time` | Failed | Collisions when multiple nodes generate IDs at the same microsecond |

**Active solution:** `primitive.NewObjectID().Hex()` generates a 24-character hex string from a 12-byte ObjectID comprising a 4-byte timestamp, 5-byte random value, and 3-byte incrementing counter. This is coordination-free and globally unique.

**Design notes:**
- The Snowflake failure is instructive: `snowflake.NewNode()` takes an `int64` but `n.ID()` returns a string. The author didn't parse the numeric suffix from the node ID.
- All four generation functions are kept in the code (not deleted), serving as documentation of the exploration process.

---

### 5.3 Challenge 3a: Single-Node Broadcast

**File:** `challenge-3a-broadcast/main.go` (54 lines)

The simplest broadcast challenge. A single logical broadcast system where nodes handle `broadcast`, `read`, and `topology` messages.

**How it works:**

A `server` struct holds the Maelstrom node and an in-memory slice:

```go
type server struct {
    n   *maelstrom.Node
    ids []float64
}
```

**Handlers:**
- **`broadcast`**: Extracts the `message` field (a float64 due to JSON number unmarshaling), appends it to `ids`, replies `broadcast_ok`
- **`read`**: Returns the entire `ids` slice in a `read_ok` response
- **`topology`**: Acknowledges with `topology_ok` but ignores the topology data entirely

**Design notes:**
- **Not thread-safe**: The `ids` slice is accessed without any synchronization. This works only because the test uses a simple workload where concurrent access is unlikely to cause visible issues, but it's technically a data race.
- **No inter-node communication**: Messages are only stored locally. This works for the basic test but won't survive multi-node validation.
- **float64 quirk**: JSON numbers are unmarshaled as `float64` by Go's `encoding/json` when the target is `any`/`interface{}`. The author uses type assertion `body["message"].(float64)`.

---

### 5.4 Challenge 3b: Multi-Node Broadcast

**File:** `challenge-3b-broadcast/main.go` (83 lines)

This challenge requires broadcast messages to propagate to all nodes in the cluster. The test runs 5 nodes.

**Key improvements over 3a:**

1. **Thread-safe storage**: Switched from `[]float64` to `map[int]struct{}` with a `sync.RWMutex`
2. **Deduplication**: Before processing a broadcast, the node checks if the message ID is already in the map. If seen, it returns `nil` immediately (silently drops the duplicate — no reply sent)
3. **Inter-node gossip**: After storing a new message, the node forwards it to every other node (excluding itself and the original sender)

**Broadcast algorithm (flooding):**

```
On receiving broadcast(message):
  1. Lock the map
  2. If message already seen → unlock, return nil (drop)
  3. Store message in map → unlock
  4. For each node in cluster (excluding self and sender):
       Send the same broadcast body to that node
  5. Reply broadcast_ok to the original sender
```

**Communication:** Uses `n.Send(dst, body)` — a fire-and-forget send with no acknowledgment or retry.

**Design notes:**
- The deduplication is critical: without it, nodes would create an infinite loop of broadcasts (A → B → A → B → ...)
- The `map[int]struct{}` pattern is idiomatic Go for implementing a set — `struct{}` takes zero bytes of storage
- **No reply on duplicates**: When a node receives a duplicate broadcast, it returns `nil` without calling `n.Reply()`. This is fine for inter-node messages (sent with `n.Send`, which doesn't expect a response) but is acceptable since Maelstrom tolerates missing responses for forwarded messages.
- **Vulnerability to message loss**: Since `n.Send` is fire-and-forget, if a message is lost in transit (e.g., due to network partition), it won't be retried. This is why 3b's test script has no `--nemesis partition` flag.

---

### 5.5 Challenge 3c: Fault-Tolerant Broadcast

**Files:** `challenge-3c-broadcast/main.go` (96 lines), `challenge-3c-broadcast/mem_store.go` (43 lines)

The most sophisticated challenge. The broadcast must survive network partitions (`--nemesis partition`). This requires reliable delivery with retries.

#### MemStore (`mem_store.go`)

A thread-safe key-value store abstracted into its own file:

```go
type MemStore struct {
    mu sync.RWMutex
    m  map[int]struct{}
}
```

**Methods:**
- `NewMemStore()` — constructor
- `Put(k int, v struct{})` — write-locks and inserts
- `IsExist(k int)` — read-locks and checks membership
- `Keys() []int` — read-locks and returns all keys as a slice

**Design note:** `Put` returns an `error` (always `nil`) — this suggests the author considered a future where the store might fail (e.g., if backed by disk or network storage), but it's unused.

#### Broadcast with Retries (`main.go`)

**Constants:**
- `retries = 100` — maximum retry attempts per destination node

**Broadcast algorithm:**

```
On receiving broadcast(message):
  1. Check if message exists in store → if yes, return nil (drop duplicate)
  2. Store message
  3. For each destination node (excluding self and sender):
       Spawn a goroutine:
         a. Define ack = false (protected by mutex)
         b. Define ackHandler that sets ack = true
         c. Loop from i=1 to retries while !ack:
              - Call n.RPC(dest, body, ackHandler)
              - Sleep for i * 100ms (linear backoff)
  4. Reply broadcast_ok immediately (don't wait for gossip to complete)
```

**Key differences from 3b:**

| Aspect | 3b | 3c |
|--------|----|----|
| Communication | `n.Send()` (fire-and-forget) | `n.RPC()` (request-response) |
| Acknowledgments | None | `ackHandler` callback |
| Retries | None | Up to 100 attempts |
| Backoff | N/A | Linear: `i * 100ms` |
| Concurrency | Sequential sends | Goroutine per destination |
| Storage | Inline `map` + `RWMutex` | Extracted `MemStore` struct |

**RPC vs Send:**
- `n.Send(dst, body)` — sends a message with no expectation of a response
- `n.RPC(dst, body, handler)` — sends a message and registers a callback (`handler`) to be invoked when the destination replies. This is how the node knows the message was received.

**Backoff calculation:**
```go
dur := time.Duration(i) * 100
time.Sleep(dur * time.Millisecond)
```
This produces: 100ms, 200ms, 300ms, ..., 10000ms (10s). This is **linear backoff**, not exponential. The total maximum wait before giving up is ~500 seconds (sum of 100ms to 10000ms), though in practice the ack arrives much sooner.

**Concurrency model:**
- Each destination gets its own goroutine, so retries to different nodes happen in parallel
- The `ack` flag and its `ackMu` mutex are local to each goroutine closure, preventing races
- The `broadcastHandler` returns immediately after spawning goroutines — it doesn't wait for gossip to complete. The client gets `broadcast_ok` right away.

**Subtlety — race between ack and retry loop:**
The `ackHandler` callback runs asynchronously when a reply arrives. Between checking `!ack` in the loop condition and calling `n.RPC`, the ack might arrive from a previous RPC call. The mutex protects the `ack` flag, but the retry loop reads `ack` without holding the lock (in the `for` loop condition). This is technically a data race, but it's benign — the worst case is one extra unnecessary RPC call.

---

## 6. Architecture and Design Patterns

### 6.1 Handler Pattern

All challenges use Maelstrom's handler registration pattern:
```go
n.Handle("message_type", handlerFunc)
```
This is an event-driven architecture where the node's `Run()` method reads JSON messages from STDIN, dispatches them to registered handlers based on the `type` field, and the handlers use `n.Reply()` to write responses to STDOUT.

### 6.2 Server Struct Pattern

Starting from challenge 3a, the code uses a `server` struct to group the node reference and state together, with handlers as methods on the struct. This avoids global variables and makes state ownership explicit.

### 6.3 Loosely-Typed Messages

All challenges use `map[string]any` for message bodies rather than defining typed structs. This trades compile-time safety for flexibility and less boilerplate. Type assertions (e.g., `body["message"].(float64)`) are used to extract values.

### 6.4 Progressive Complexity

The project demonstrates a clear learning progression:

```
Stateless          → Stateful (no sync)    → Stateful (sync.RWMutex)    → Abstracted store
No communication   → Fire-and-forget       → RPC with ack + retry
Single node        → Multi-node flooding   → Fault-tolerant flooding
```

---

## 7. Concurrency and Thread Safety

| Challenge | Thread Safety | Mechanism |
|-----------|--------------|-----------|
| 1 (Echo) | N/A (stateless) | — |
| 2 (Unique IDs) | N/A (stateless) | External libraries handle it |
| 3a | Not thread-safe | Bare `[]float64` slice |
| 3b | Thread-safe | `sync.RWMutex` on `map[int]struct{}` |
| 3c | Thread-safe | `sync.RWMutex` inside `MemStore` |

In 3b, the lock is acquired and released manually (not using `defer`) in the broadcast handler. This allows the lock to be released before sending messages to other nodes, avoiding holding the lock during I/O.

In 3c, all `MemStore` methods use `defer` for unlock, which is safer but means the lock is held slightly longer.

---

## 8. Distributed Systems Concepts Demonstrated

### Flooding / Gossip Protocol
Challenges 3b and 3c implement a **flooding broadcast**: every node forwards every new message to every other node. This is O(n²) in messages but simple and correct.

### Deduplication
The `map[int]struct{}` set prevents infinite message loops and redundant processing. Without deduplication, the flooding algorithm would loop forever.

### Idempotency
The broadcast handlers are idempotent — receiving the same message twice has no effect beyond the first time (checked via the set).

### At-Least-Once Delivery
Challenge 3c implements at-least-once semantics through retries. Combined with idempotent handlers, this achieves effectively-once processing.

### Coordination-Free Unique IDs
Challenge 2 demonstrates that globally unique IDs can be generated without consensus or coordination, using randomness (UUID) or embedded machine identity (ObjectID).

### Network Partition Tolerance
Challenge 3c's retry mechanism ensures that messages buffered during a partition are eventually delivered when the partition heals.

---

## 9. Git History and Evolution

The entire project was built in two days:

**Day 1 (April 14, 2023):**
| Time | Commit | Description |
|------|--------|-------------|
| 09:01:20 | `6ff6303` | README |
| 09:01:58 | `80c8f05` | Challenge 1: Echo |
| 09:02:27 | `18e3e70` | Challenge 2: Unique IDs |
| 09:02:47 | `c6afaf3` | Challenge 3a: Basic Broadcast |
| 09:03:15 | `621e5fe` | Challenge 3b: Multi-Node Broadcast |

**Day 2 (April 15, 2023):**
| Time | Commit | Description |
|------|--------|-------------|
| 11:16:54 | `e3c6012` | Refactored test script paths (portability fix) |
| 11:18:35 | `26b7285` | Added compiled binaries |
| 11:19:04 | `8615a26` | Refactored challenge 2 (comments, cleanup) |
| 11:20:31 | `f4cca09` | Challenge 3c: Fault-tolerant Broadcast |

All development happened on `main` with a linear history — no feature branches.

---

## 10. Observations and Potential Improvements

### What works well
- Clean, readable code with minimal abstraction
- Progressive complexity that mirrors the learning journey
- Comments documenting what was tried and what failed (challenge 2)
- Proper use of `sync.RWMutex` for read-heavy workloads

### Potential improvements

1. **Challenge 3a is not thread-safe**: The `ids` slice is mutated without synchronization. Maelstrom handlers can run concurrently, so this is a latent data race.

2. **Challenge 3b — no reply on duplicates**: When a node-to-node broadcast is a duplicate, the handler returns `nil` without replying. For messages sent via `n.Send()` this is fine, but if the Maelstrom client ever sends a duplicate, it won't get a response.

3. **Challenge 3c — benign data race on `ack`**: The `for` loop reads `ack` without holding `ackMu`. While practically harmless (worst case: one extra RPC), it could be flagged by Go's race detector.

4. **Topology is ignored**: All three broadcast challenges receive a `topology` message describing the network graph but never use it. The current flooding approach sends to *all* nodes. Using the topology to build a spanning tree or gossip tree would reduce message complexity from O(n²) to O(n).

5. **Compiled binaries in git**: The `maelstrom-*` binaries are committed to the repository. These are platform-specific and would ideally be in `.gitignore`.

6. **Missing challenges**: The Gossip Glomers suite includes challenges 4 (Grow-Only Counter), 5 (Kafka-Style Log), and 6 (Totally-Available Transactions). These are not implemented.

7. **Backoff in 3c is linear, not exponential**: `i * 100ms` grows linearly. Exponential backoff (`100ms * 2^i`) would be more standard for distributed systems retries, reducing load during extended partitions.

---

## 11. Summary

This project is a focused, well-structured implementation of the first three Gossip Glomers challenges in Go. It demonstrates a clear progression from trivial message handling (echo) through coordination-free ID generation (unique IDs) to fault-tolerant distributed broadcast with retries and deduplication. The code is concise, idiomatic Go that prioritizes readability over abstraction. The most interesting technical evolution is in the broadcast challenges, where each iteration addresses a specific distributed systems concern: state management (3a), consistency via deduplication and gossip (3b), and fault tolerance via acknowledgment-driven retries (3c).
