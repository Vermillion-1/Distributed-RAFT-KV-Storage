# 🏛️ Raft Explained — Plain English

You don't need to understand every detail. You need enough to understand *why* the code does what it does.

---

## The Problem: Distributed Agreement

Imagine 3 people (nodes) need to agree on a shared notepad. Anyone can propose writing something, but only if the majority agrees does the write become permanent.

The question: **how do they agree when anyone can fail at any time?**

---

## The Answer: Raft

Raft solves this with 3 rules:

### Rule 1 — There Is Always One Leader
One node is the **Leader**. All writes go through it. The others are **Followers** who just copy what the Leader says.

```
kv-client → Leader  ← "set foo=bar"
               ↓
           node1 (follower) ← "append entry to log"
           node2 (follower) ← "append entry to log"
               ↓
Leader: "majority confirmed, commit it"
```

### Rule 2 — Heartbeats Keep the Leader Alive
The Leader sends a heartbeat to every Follower every 200ms (in this project).  
If a Follower misses ~5 heartbeats (≈1 second), it assumes the Leader is dead and starts an election.

### Rule 3 — Elections Require a Majority Vote
To become the new Leader, a Candidate needs votes from **more than half** the cluster.

```
3-node cluster: need 2/3 votes  ← quorum = ⌊3/2⌋+1 = 2
5-node cluster: need 3/5 votes  ← quorum = ⌊5/2⌋+1 = 3
11-node cluster: need 6/11 votes ← quorum = ⌊11/2⌋+1 = 6
```

**Key insight:** if you only have 1 node alive out of 3, there can be no election (can't get 2/3 votes). The cluster safely stops accepting writes rather than risk inconsistency. This is the CP guarantee.

---

## The Raft Log

Every write is an **entry** in a log. Think of it like a shared Google Doc — every change is a timestamped entry, and all nodes must replicate the same log in the same order.

```
Index 1:  set("greeting", "hello")
Index 2:  set("city", "Vancouver")
Index 3:  delete("greeting")
Index 4:  set("foo", "bar")      ← applied_index = 4
```

The `applied_index` you see in the dashboard is how far along this log a node has replicated.

---

## What Happens When a Node Crashes

```
Before crash: node0 (Leader, idx=50), node1 (idx=50), node2 (idx=50)

node0 crashes →

node1 starts election
node1 gets vote from node2
node1 becomes Leader

New writes go to node1
node1 (Leader, idx=55), node2 (idx=55)

node0 restarts →
node0 sees it's behind (idx=50 vs leader's 55)
Raft sends it entries 51–55
node0 catches up to idx=55
```

---

## What Happens During a Network Partition (SIGSTOP)

```
node0 frozen (SIGSTOP) →
node0 can't send heartbeats
node1 & node2 time out

node1 starts election
node1 gets vote from node2 (majority of remaining = 2/2)
node1 becomes Leader

node0 resumes (SIGCONT) →
node0 sees higher term number in node1's messages
node0 immediately steps down to Follower
node0 catches up via log replication
```

The **term number** is Raft's "generation counter" — it increments on every election. A node from an older term always defers to a node with a newer term.

---

## Key Terms

| Term | Meaning |
|---|---|
| **Leader** | The node that accepts writes and sends heartbeats |
| **Follower** | Passive replica — copies the leader's log |
| **Candidate** | A follower that started an election |
| **Term** | A monotonically increasing round number. Each election increments it |
| **Applied Index** | How many log entries a node has applied to its state machine |
| **Quorum** | The minimum number of nodes needed to make a decision (⌊N/2⌋+1) |
| **FSM** | Finite State Machine — the actual KV map that Raft drives |
| **Heartbeat** | A periodic message the Leader sends to prevent elections |
| **MTTR** | Mean Time To Recovery — how long it takes to elect a new leader |

---

## Why This Matters for Your Tasks

**Phase 3** tests what happens when nodes are *slow* but not dead:
- A slow follower shouldn't hold back the leader (because quorum only needs a majority)
- A slow leader forces clients to wait (writes go through the leader)

**Phase 4** tests what happens to *data* when nodes crash:
- Data should survive crashes because it's written to BoltDB before acknowledged
- Snapshots allow nodes to recover without replaying the entire log from the start
