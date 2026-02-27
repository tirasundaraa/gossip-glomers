# Gossip Glomers - Project Research Report

## Overview

This is a Go implementation of the [Gossip Glomers](https://fly.io/dist-sys/) distributed systems challenges, built on top of the [Maelstrom](https://github.com/jepsen-io/maelstrom) testing framework (from the Jepsen ecosystem). The project implements distributed nodes that communicate via a JSON-over-STDIN/STDOUT protocol, and Maelstrom verifies their correctness under various failure conditions (network partitions, crashes, etc.).

The repository has two distinct layers of history: an older set of standalone challenge implementations (now in `archived/`), and a current, cleaner monolithic binary where all handlers live in one Go module.

---

## Repository Structure

```
gossip-glomers/
├── cmd/node/main.go            # Single entry point - registers all handlers, runs event loop
├── internal/node/
│   ├── node.go                 # Server struct + NewServer constructor
│   ├── echo.go                 # Challenge 1: Echo handler
│   └── unique_id.go            # Challenge 2: Unique ID handler
├── archived/                   # Previous standalone implementations (historical)
│   ├── challenge-1-echo/
│   ├── challenge-2-unique-ids/
│   ├── challenge-3a-broadcast/
│   ├── challenge-3b-broadcast/
│   └── challenge-3c-broadcast/
├── maelstrom/                  # Vendored Maelstrom binary + Go library + docs
├── store/                      # Test result artifacts (gitignored)
├── docs/                       # Challenge write-ups
├── Makefile                    # Build + test automation
├── go.mod / go.sum             # Go module (single dependency: jepsen-io/maelstrom)
└── .gitignore                  # Ignores maelstrom/, bin/, store/
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

The current code follows a clean separation:

- **`cmd/node/main.go`** - Entry point. Creates a `Server`, registers handlers for `"echo"` and `"generate"` message types, then calls `Node.Run()` which blocks on the STDIN event loop.
- **`internal/node/node.go`** - Defines `Server` struct wrapping a `*maelstrom.Node` and an `atomic.Int64` counter. The counter is used for unique ID generation.
- **`internal/node/echo.go`** - Echo handler.
- **`internal/node/unique_id.go`** - Unique ID generation handler.

All handlers are methods on `*Server`, giving them shared access to the node and counter state.

### Challenge 1: Echo

The simplest challenge. Receives a message, sets `type` to `"echo_ok"`, and replies. The entire original body (including the `echo` field) is preserved and sent back. This validates that the node can correctly participate in the Maelstrom protocol.

**Test parameters**: 1 node, 10 seconds.
**Results**: 47 operations, 100% success, 2.04 messages per operation (one request + one response).

### Challenge 2: Unique ID Generation

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

**Trade-offs documented by the author**:
- String IDs are longer than 64-bit Snowflake IDs (less efficient to index in a DB).
- Clock dependency means IDs aren't strictly time-ordered across nodes (clock skew), though uniqueness is unaffected thanks to the NodeID prefix.

**Test parameters**: 3 nodes, 30 seconds, 1000 requests/second, total availability required, network partitions injected.
**Results**: 24,713 operations, 0 failures, 0 duplicates, 2.00 messages per operation. ID range from `"n0-1772102102555726000-1"` to `"n2-1772102132547693000-6227"`.

---

## Archived Implementations (Historical Evolution)

The `archived/` directory preserves earlier standalone implementations. Each was its own Go module. Studying them reveals the learning progression:

### Challenge 1 (archived): Echo

Identical logic to the current version, but as a flat `main.go` with an inline closure handler instead of a method on a struct.

### Challenge 2 (archived): Unique IDs - Experimentation

The archived version reveals the author's experimentation with multiple ID generation strategies:

1. **Google UUID (`uuid.New().String()`)** - Passed. Relies on cryptographic randomness. Works but is heavier than needed.
2. **MongoDB ObjectID (`primitive.NewObjectID().Hex()`)** - Passed. 12-byte IDs combining timestamp + machine ID + process ID + counter. The final chosen approach in the archive.
3. **Twitter Snowflake (`snowflake.Node.Generate()`)** - **Did not work**. Required passing the Node ID as an integer, but Maelstrom assigns string IDs like `"n0"`. The library expected a numeric node identifier.
4. **Timestamp only (`time.Now().UnixMicro()`)** - **Did not work**. Collisions between nodes generating the same microsecond timestamp. No spatial uniqueness.

The current implementation supersedes all of these with the simpler composite key approach that has zero external dependencies.

### Challenge 3a (archived): Single-Node Broadcast

A naive single-node implementation:
- Stores broadcast messages in a plain `[]float64` slice (not thread-safe).
- `broadcast`: appends the message value to the slice, replies `broadcast_ok`.
- `read`: returns the entire slice.
- `topology`: acknowledges but ignores the topology.

This only works for single-node tests because there's no inter-node communication, and the slice isn't protected by a mutex (a data race with Maelstrom's concurrent handler dispatch).

### Challenge 3b (archived): Multi-Node Broadcast

Adds multi-node support with several key improvements:

1. **Thread-safe storage**: switched from `[]float64` to `map[int]struct{}` protected by `sync.RWMutex`.
2. **Deduplication**: checks if a message was already seen before processing. This prevents infinite broadcast loops.
3. **Flooding protocol**: on receiving a new broadcast, forwards it to every other node in the cluster (excluding self and the sender) using `Node.Send()` (fire-and-forget).

The deduplication-before-forward pattern is critical: without it, A sends to B, B sends back to A, A sends to B... forever.

Weakness: `Node.Send()` is fire-and-forget. If a message is dropped (network partition), it's lost permanently. This passes the basic multi-node test but fails under partitions.

### Challenge 3c (archived): Fault-Tolerant Broadcast

The most sophisticated archived implementation. Addresses message loss with:

1. **`MemStore`**: A dedicated thread-safe key-value store (`sync.RWMutex` + `map[int]struct{}`), cleanly separated into its own file. Provides `Put`, `IsExist`, and `Keys` operations.
2. **RPC with retries**: Instead of `Send()`, uses `Node.RPC()` which registers a callback. The callback sets an `ack` flag (protected by its own mutex) when a response arrives.
3. **Goroutine-per-peer**: Each broadcast spawns a goroutine per target peer that retries up to 100 times.
4. **Linear backoff**: Wait time increases linearly: `i * 100ms` on the i-th retry (100ms, 200ms, 300ms...). Max wait would be 10 seconds on the 100th retry.
5. **Non-blocking**: The reply to the client is sent immediately after storing locally; retries happen asynchronously in background goroutines.

The retry logic has a subtle detail: `Node.RPC()` is asynchronous - it sends the message and registers a callback for later. The loop checks `!ack` before each retry, so once the callback fires and sets `ack = true`, the loop exits. The `ackMu` mutex prevents a race between the callback goroutine and the retry loop reading `ack`.

---

## Build System

The `Makefile` provides three targets:

| Target | What It Does |
|---|---|
| `build` | `go build -o bin/node ./cmd/node` |
| `challenge-1` | Builds, then runs Maelstrom echo test (1 node, 10s) |
| `challenge-2` | Builds, then runs Maelstrom unique-ids test (3 nodes, 30s, 1000 req/s, partitions) |
| `clean` | Removes `bin/` and `store/` |

Maelstrom test flags explained:
- `-w <workload>`: which workload to run (echo, unique-ids, broadcast, etc.)
- `--bin <path>`: path to the node binary
- `--node-count N`: how many node processes to spawn
- `--time-limit N`: test duration in seconds
- `--rate N`: target requests per second
- `--availability total`: every request must succeed (even during partitions)
- `--nemesis partition`: inject network partitions during the test

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
- **Workload-specific**: e.g., no duplicate IDs, all echoes correct
- **Performance**: latency and rate graphs generated successfully
- **Network**: message counts and per-operation message overhead
- **Exceptions**: no unhandled crashes

---

## Concurrency Model

The Maelstrom Go library dispatches each message to a new goroutine. This has direct implications:

1. **All shared state must be thread-safe.** The `Server.counter` uses `atomic.Int64` (lock-free). The archived broadcast implementations use `sync.RWMutex`-protected maps.
2. **STDOUT writes are serialized** by a mutex in `Node.Send()`, preventing interleaved JSON output.
3. **STDIN reads are sequential** on the main goroutine (single scanner loop), so message ordering is preserved at the input level.
4. **Callbacks are goroutine-dispatched too** - RPC response handlers run in their own goroutines, requiring their own synchronization (as seen in the 3c archived broadcast's `ackMu`).

---

## Distributed Systems Concepts Demonstrated

| Concept | Where It Appears |
|---|---|
| Coordination-free design | Unique ID generation needs no inter-node communication |
| CAP theorem (AP) | Unique IDs remain available during partitions |
| Gossip / flooding protocol | Challenge 3b broadcasts to all peers |
| Idempotency / deduplication | Challenge 3b/3c check `IsExist` before processing |
| Retry with backoff | Challenge 3c retries with linear backoff |
| Async RPC with callbacks | Challenge 3c uses `Node.RPC()` for acknowledged delivery |
| CRDTs (grow-only set) | The broadcast store is essentially a G-Set - only additions, merge = union |
| Fire-and-forget vs. acknowledged delivery | 3b uses `Send()` (lossy), 3c uses `RPC()` (acknowledged) |

---

## Observations

### Design Decisions

- **Single binary, multiple handlers**: The current architecture registers all challenge handlers on one binary. This is clean but means the binary grows with each challenge. Maelstrom only sends the message types relevant to the workload being tested, so unused handlers are inert.
- **No external dependencies in current code**: The refactored version dropped UUID, Snowflake, and MongoDB libraries in favor of a zero-dependency composite key. Simpler and more educational.
- **Atomic vs. Mutex**: The unique ID handler uses `atomic.Int64` instead of a mutex-protected counter. This is a good choice - atomic operations are lock-free and significantly faster for simple increment operations.

### Potential Improvements

- **Challenge 3 not yet ported**: The broadcast challenges exist only in `archived/`. Porting them to the current `internal/node/` structure would complete the project.
- **Broadcast 3c's backoff**: Linear backoff (`i * 100ms`) is simple but not optimal. Exponential backoff with jitter would reduce network congestion during partitions.
- **Broadcast 3c's topology**: All three broadcast implementations ignore the `topology` message and flood to all nodes. Using the provided topology (e.g., a spanning tree) would reduce message overhead, which is relevant for later challenges (3d, 3e) that have strict message budget constraints.
- **No tests**: The project has no Go unit tests. The Maelstrom integration tests serve as end-to-end validation, but unit tests for individual handlers would be straightforward to write (the library supports injecting mock STDIN/STDOUT).

### Git History

The project evolved in two phases:

1. **Phase 1** (commits `6ff6303` through `f4cca09`): Individual challenge solutions, each as a standalone module. Progressed from echo through broadcast with partitions.
2. **Phase 2** (commits `aab8142` through `d43efaa`): Archived all old implementations, restructured into `cmd/internal` layout, re-implemented challenges 1-2 with cleaner code.

---

## Available Maelstrom Workloads (Not Yet Attempted)

Based on the Maelstrom documentation, the following workloads remain:

| Workload | Description |
|---|---|
| Broadcast (3d/3e) | Efficiency-constrained broadcast (message budgets, latency targets) |
| G-Counter | Grow-only counter (CRDT) |
| G-Set | Grow-only set (CRDT) |
| Pn-Counter | Positive-negative counter (increment + decrement) |
| Kafka | Append-only log with offsets, polls, and committed offsets |
| Lin-kv | Linearizable key-value store |
| Txn-list-append | Transactional list-append workload |
| Txn-rw-register | Transactional read-write register workload |

Maelstrom also provides built-in services (`lin-kv`, `seq-kv`, `lww-kv`, `lin-tso`) that nodes can use as building blocks for more complex systems - for example, using the linearizable KV store as a foundation for a transaction protocol.
