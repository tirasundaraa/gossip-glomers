# Challenge #4: Grow-Only Counter (G-Counter)

## The Objective

The goal is to implement a distributed **Grow-Only Counter** (a state-based CRDT) that remains available and correct even in the presence of network partitions.

### Key Constraints:

* **High Availability:** The system must process requests even when nodes are isolated from each other.
* **Monotonicity:** The counter value must never decrease. In a distributed environment, "stale reads" can lead to accidental value regression; this implementation must mathematically prevent that.

---

## Architectural Design & Trade-offs

### 1. Externalizing State via `seq-kv`

We utilize Maelstrom's **Sequential Key-Value (seq-kv)** store.

* **Benefit:** Keeps our individual nodes stateless, allowing for easy scaling and recovery.
* **Trade-off:** Introducing an external dependency makes the `seq-kv` service a potential Single Point of Failure (SPOF). We mitigate this by treating the network to the KV store as unreliable and implementing robust retry logic.

### 2. Conflict-Free Writes via Key Partitioning

To avoid the complexity of distributed locking, we partition the data.

* **Strategy:** Each node ($n_i$) is the **sole writer** for its own key (e.g., node `n0` only writes to key `"n0"`).
* **Result:** This eliminates cross-node write conflicts. Node `n1` never has to worry about Node `n2` overwriting its local progress.

---

## Implementation Details

### 1. Concurrent Read Aggregation

Since the global total is split across $N$ keys, a `read` operation must sum all values. To prevent latency from scaling linearly ($O(N)$) with the number of nodes, we use **Parallel Fetching**.

* **Concurrency:** We spawn a goroutine for every node in the cluster to fetch its specific key simultaneously.
* **Synchronization:** A `sync.WaitGroup` ensures the handler waits for all "interns" to return before replying.
* **Efficiency:** Instead of a heavy `sync.Mutex` to sum the results, we use `atomic.Int64.Add()`. This leverages native CPU instructions for thread-safe addition, providing better performance under high concurrency.

### 2. Resilient Read-Modify-Write (RMW)

Even though only one node writes to its own key, we still face the **Stale Read** problem. If a node reads its own value, adds a delta, and writes it back, it might accidentally overwrite a previous successful write if the initial read was stale.

We solved this using **Optimistic Concurrency Control (OCC)**:

* **Compare-And-Swap (CAS):** Instead of a blind `Write()`, we use `CompareAndSwap()`. This ensures the update only succeeds if the value in the store matches the value we last read.
* **Recovery:** If CAS fails (Code 22: `PreconditionFailed`), the node recognizes its local state is stale, re-fetches the latest value from the KV store, and retries the calculation.

### 3. Fault-Tolerant Networking

Both `read` and `add` operations are wrapped in a **Context-Aware Retry Loop**:

* **Transient Error Handling:** Errors like `Timeout` or `TemporarilyUnavailable` trigger a retry with a 100ms backoff.
* **Fatal Error Handling:** Permanent errors (e.g., `KeyDoesNotExist`) are handled gracefully by defaulting to `0` or exiting immediately to prevent infinite loops.
* **Master Timeout:** A global 5-second `context.Context` acts as a "circuit breaker," ensuring that if a network partition is permanent, the node returns an error to the client rather than deadlocking or leaking resources.

---

## Results & Performance

The implementation successfully passed the Maelstrom G-Counter workload with **Network Partitioning (Nemesis)** enabled.

* **Consistency:** Final reads across all nodes converged to the exact same value.
* **Availability:** 100% of requests were acknowledged (OK fraction: 1.0).
* **Reliability:** The system maintained monotonicity (the counter never went backwards) despite simulated network instability.

| Metric | Result |
| --- | --- |
| **Total Ops** | 1,703 |
| **Availability** | 100% |
| **Consistency** | Strong (Final Convergence) |
| **Max Network Hops** | 1 (Direct to KV) |
