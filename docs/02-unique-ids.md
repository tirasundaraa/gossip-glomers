# Challenge 2: Unique ID Generation

## The Goal
Implement a globally unique ID generator that is:
1.  **Totally Available:** Must function during network partitions (CAP Theorem: AP system).
2.  **High Throughput:** Must handle thousands of requests per second.
3.  **Crash Safe:** Must not generate duplicates even if a node crashes and restarts.

## The Solution: Composite Keys
We avoided a central coordinator (like a database or a "leader" node) to prevent Single Points of Failure (SPOF) and network bottlenecks.

### Strategy: `NodeID` + `Timestamp` + `Sequence`

We construct IDs by combining three sources of entropy:
`id = fmt.Sprintf("%s-%d-%d", nodeID, nowNano, counter)`

### Why this works
1.  **Spatial Uniqueness (NodeID):**
    * Maelstrom assigns unique IDs (`n1`, `n2`...).
    * This guarantees that Node A never generates an ID that Node B could generate.
2.  **Temporal Uniqueness (Timestamp):**
    * **Problem:** If a node crashes, memory is wiped. An in-memory counter would reset to 0, causing duplicates of IDs generated before the crash.
    * **Fix:** Using `time.Now().UnixNano()` ensures that the "prefix" of the ID is always moving forward. A restarted node will wake up at a later time than when it crashed.
3.  **Local Concurrency (Atomic Sequence):**
    * **Problem:** Multiple requests can arrive within the same nanosecond.
    * **Fix:** An `atomic.AddUint64` counter ensures strict ordering for requests happening at the exact same moment.

## Trade-offs
* **String Length:** The IDs are relatively long strings. In a DB, this might be less efficient to index than a 64-bit integer (Snowflake).
* **Clock Dependency:** We rely on the system clock. While clock skew doesn't break uniqueness (thanks to NodeID), it does mean IDs aren't strictly time-ordered across nodes.
