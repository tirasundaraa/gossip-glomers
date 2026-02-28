# Challenge 3d: Efficient Broadcast Part 1

## Overview

In Challenge 3d, the cluster size was increased to 25 nodes, and strict performance limits were introduced:

* **Messages-per-operation:** Below 30
* **Median latency:** Below 400ms
* **Maximum latency:** Below 600ms

Maelstrom also introduced `--latency 100`, meaning every single network hop took exactly 100ms. We could no longer rely on our "shout everything to everyone" gossip protocol from 3b and 3c, as it resulted in massive network congestion, 21-second latencies, and over 80 messages per operation.

## The Architectural Evolution

We iterated through several distributed system paradigms to solve this:

### 1. The Reliable Batching Queue

First, we decoupled the foreground client requests from the background network requests.
Instead of instantly sending a network message for every client broadcast, we added the message to a `pending` bucket (using a `map[int64]struct{}` as a thread-safe Set). A background worker (the Flusher) ticked periodically, scooped up all pending messages, and sent them as a single batched array. This drastically reduced the number of network frames sent.

### 2. The Failed Epidemic Gossip

We attempted to use **Randomized Epidemic Gossip**, where nodes forward batches to $k$ random nodes in the cluster.

* **The Problem:** We hit the mathematical limits of probability. If $k$ was too high (e.g., 8), we created a "Broadcast Storm" that clogged the network, leading to 21-second delays. If $k$ was too low (e.g., 3), the rumor faded out before reaching everyone (the "Wallflower" problem), resulting in permanently lost messages.

### 3. The Solution: A Wide Spanning Tree

To achieve mathematically perfect efficiency, we overrode Maelstrom's provided Grid topology and constructed a **Spanning Tree** in our `TopologyHandler`.

By mapping the 25 nodes into a wide, shallow tree (a fan-out of 10 children per parent), we achieved two things:

1. **Zero Duplicate Messages:** A node only ever receives a message from its parent or children, never redundantly. Our messages-per-operation dropped to **~15**.
2. **Minimal Hops:** With a fan-out of 10, the maximum depth of the tree is 2. The longest journey for any message is 4 hops. Combined with a fast 50ms ticker for our batch flusher, our median latency dropped to **~360ms**, passing the constraints.

## The SPOF Trade-off (Important Architectural Note)

While the Spanning Tree passes the strict mathematical constraints of Challenge 3d, it introduces a severe real-world vulnerability: **A Single Point of Failure (SPOF)**.

Node 0 (`n0`) acts as the root. If this node were to crash or experience a network partition, the tree would sever, and the cluster would lose the ability to achieve eventual consistency. We accepted this trade-off only because Challenge 3d tests efficiency on a stable network. In a highly available production system (an AP system), the Epidemic Gossip model is preferred to eliminate the SPOF, trading a bit of network efficiency for extreme fault tolerance.
