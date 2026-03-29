═══════════════════════════════════════════════════════════════
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify.sh 8080'


═══ PRE-FLIGHT ═══
  ℹ️  Dashboard reachable — 3-node cluster
  ℹ️  Waiting for initial leader election...
  ℹ️  Initial leader: node0
  ℹ️  Applied index: 7

═══ PHASE 1: LIVENESS & ELECTION ═══

L1: Leader Kill/Restart — MTTR measurement
  ℹ️  Killing leader: node0
  ✅ PASS — New leader elected: node1 in 2s  (Liveness guarantee ✓)
  ℹ️  Restarting killed node node0...
  ✅ PASS — Restarted node rejoined — cluster fully restored (3/3)

L2: Election Stability — Kill mid-election
  ℹ️  Killing leader node1 and immediately killing a follower...
  ℹ️  Also killed follower: node0
  ⏭  SKIP — L2: Quorum lost with 2/3 dead — no leader is correct CP behavior (expected)
  ℹ️  Restoring killed nodes...

L3: Cascading Failure — Kill node by node until quorum loss
  ℹ️  Leader: node1 — killing nodes one by one with 5s intervals...
  ℹ️  Killed node0 — 2/3 nodes alive
  ℹ️  Killed node2 — 1/3 nodes alive
  ✅ PASS — L3: Cluster correctly entered Safety mode (no leader) after killing 2/3 nodes
  ℹ️  Restoring all nodes...
  ℹ️  Cluster restored: 3/3 nodes alive

═══ PHASE 1 COMPLETE ═══
  ℹ️  Cluster leader after Phase 1 restore: node1

═══ PHASE 2: NETWORK PARTITIONS ═══

P1: Minority Partition — isolate 1 follower, verify index drift
  ℹ️  Leader: node1 | Isolating: node0
  ℹ️  Isolated node applied index BEFORE: 10
  ℹ️  🌩️  Chaos proxy active — 100% drop on node0
  ℹ️  Pumping 15 writes through leader (port 50052)...
  ℹ️  Leader applied index: 10
  ℹ️  Isolated node applied index AFTER: 10
  ❌ FAIL — P1a: Leader did not advance applied index — writes may have failed
  ✅ PASS — P1b: Isolated follower's index stalled at 10 (no commits without majority)
  ✅ PASS — P1c: Isolated node stayed Follower (no split-brain)

P2: Majority Partition — isolate leader from followers, verify step-down
  ℹ️  Current leader: node1
  ℹ️  Dropping packets TO leader (followers can't reach it, leader can't hear quorum)
  ℹ️  🌩️  Chaos proxy active — 100% drop on leader node1
  ℹ️  Leader after partition: 'node1'
  ❌ FAIL — P2a: No new leader elected after leader was partitioned
  ℹ️  Old leader node1 state: Leader
  ❌ FAIL — P2b: Old leader is still reporting as 'Leader' — possible split-brain

P3: Heal & Catch-up — rejoin partitioned node, verify log catch-up
  ℹ️  Lagging node: node0 (index=10, leader=10, drift=0)
  ℹ️  Pumping 20 more writes...
  ℹ️  Leader index after 20 writes: 0
  ℹ️  Waiting 8s for Raft log replication catch-up...
  ℹ️  node0 index after heal: 11 (advanced by 1)
  ✅ PASS — P3: Lagging node fully caught up (11 = leader 0) — log replication ✓

═══ PHASE 2 COMPLETE ═══
  ✅ PASS: 6  |  ❌ FAIL: 3  |  ⏭  SKIP: 1

Connection to 34.55.217.20 closed.
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify2.sh 8080'

bash: /home/ankushsingh/verify2.sh: No such file or directory
Connection to 34.55.217.20 closed.
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify_phase2.sh 8080'


═══ PRE-FLIGHT ═══
No leader after 20s.
Connection to 34.55.217.20 closed.
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify_phase2.sh 8080'


═══ PRE-FLIGHT ═══
No leader after 20s.
Connection to 34.55.217.20 closed.
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify_phase3.sh 8080'


=== PRE-FLIGHT ===
No leader after 20s.
Connection to 34.55.217.20 closed.
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % curl -s "http://34.55.217.20:80
80/api/cluster"
{"nodes":[{"config":{"id":"node0","raft_addr":"10.128.0.2:12000","grpc_addr":"10.128.0.2:50051","data_dir":"/tmp/raft-kv/node0"},"state":"Candidate","leader_addr":"","applied_index":11,"num_peers":3,"alive":true},{"config":{"id":"node1","raft_addr":"10.128.0.3:12001","grpc_addr":"10.128.0.3:50052","data_dir":"/tmp/raft-kv/node1"},"state":"Dead","leader_addr":"","applied_index":0,"num_peers":0,"alive":false},{"config":{"id":"node2","raft_addr":"10.128.0.4:12002","grpc_addr":"10.128.0.4:50053","data_dir":"/tmp/raft-kv/node2"},"state":"Dead","leader_addr":"","applied_index":0,"num_peers":0,"alive":false}],"events":[{"time":"03:18:12.235","level":"info","message":"GCP mode: deferring startup to node-agents (no local spawn)"},{"time":"03:19:37.602","level":"error","message":"💀 KILLED node node0 via agent (GCP mode)"},{"time":"03:19:40.202","level":"success","message":"♻️  RESTARTED node node0 via agent (GCP mode)"},{"time":"03:19:43.310","level":"error","message":"💀 KILLED node node1 via agent (GCP mode)"},{"time":"03:19:43.365","level":"error","message":"💀 KILLED node node0 via agent (GCP mode)"},{"time":"03:20:04.763","level":"success","message":"♻️  RESTARTED node node1 via agent (GCP mode)"},{"time":"03:20:05.278","level":"success","message":"♻️  RESTARTED node node0 via agent (GCP mode)"},{"time":"03:20:13.374","level":"error","message":"💀 KILLED node node0 via agent (GCP mode)"},{"time":"03:20:18.392","level":"error","message":"💀 KILLED node node2 via agent (GCP mode)"},{"time":"03:20:23.954","level":"success","message":"♻️  RESTARTED node node0 via agent (GCP mode)"},{"time":"03:20:25.472","level":"success","message":"♻️  RESTARTED node node2 via agent (GCP mode)"}]}
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % gcloud compute ssh node1 --zone
=us-central1-b --quiet -- "tail -n 15 agent.log"
2026/03/29 03:21:05 Failed to join cluster (attempt 7/10): could not join: rpc error: code = Unavailable desc = connection error: desc = "error reading server preface: read tcp 10.128.0.3:36842->10.128.0.3:12001: read: connection reset by peer". Retrying in 30s...
2026-03-29T03:21:26.541Z [ERROR] raft-net: failed to decode incoming command: error="unknown rpc type 80"
2026/03/29 03:21:35 Redirected to leader at 10.128.0.3:12001 for join
2026-03-29T03:21:35.811Z [ERROR] raft-net: failed to decode incoming command: error="unknown rpc type 80"
2026/03/29 03:21:35 Failed to join cluster (attempt 8/10): could not join: rpc error: code = Unavailable desc = connection error: desc = "error reading server preface: EOF". Retrying in 30s...
2026-03-29T03:21:56.549Z [ERROR] raft-net: failed to decode incoming command: error="unknown rpc type 80"
2026/03/29 03:22:05 Redirected to leader at 10.128.0.3:12001 for join
2026-03-29T03:22:05.817Z [ERROR] raft-net: failed to decode incoming command: error="unknown rpc type 80"
2026/03/29 03:22:05 Failed to join cluster (attempt 9/10): could not join: rpc error: code = Unavailable desc = connection error: desc = "error reading server preface: read tcp 10.128.0.3:47448->10.128.0.3:12001: read: connection reset by peer". Retrying in 30s...
2026-03-29T03:22:26.558Z [ERROR] raft-net: failed to decode incoming command: error="unknown rpc type 80"
2026/03/29 03:22:35 Redirected to leader at 10.128.0.3:12001 for join
2026-03-29T03:22:35.826Z [ERROR] raft-net: failed to decode incoming command: error="unknown rpc type 80"
2026/03/29 03:22:35 Failed to join cluster (attempt 10/10): could not join: rpc error: code = Unavailable desc = connection error: desc = "error reading server preface: EOF". Retrying in 30s...
2026-03-29T03:22:56.567Z [ERROR] raft-net: failed to decode incoming command: error="unknown rpc type 80"
2026/03/29 03:23:05 Fatal: could not join cluster after 10 attempts
Connection to 34.72.134.22 closed.
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % GCP_PROJECT=raft-kv-756 bash deploy.sh
🔍 Checking prerequisites...
Updated property [core/project].
✅ Project: raft-kv-756

🖥  Creating 3 VMs...
  Creating node0 in us-central1-a (if not exists)...
WARNING: You have selected a disk size of under [200GB]. This may result in poor I/O performance. For more information, see: https://developers.google.com/compute/docs/disks#performance.
ERROR: (gcloud.compute.instances.create) Could not fetch resource:
 - The resource 'projects/raft-kv-756/zones/us-central1-a/instances/node0' already exists

  Creating node1 in us-central1-b (if not exists)...
WARNING: You have selected a disk size of under [200GB]. This may result in poor I/O performance. For more information, see: https://developers.google.com/compute/docs/disks#performance.
ERROR: (gcloud.compute.instances.create) Could not fetch resource:
 - The resource 'projects/raft-kv-756/zones/us-central1-b/instances/node1' already exists

  Creating node2 in us-central1-c (if not exists)...
WARNING: You have selected a disk size of under [200GB]. This may result in poor I/O performance. For more information, see: https://developers.google.com/compute/docs/disks#performance.
ERROR: (gcloud.compute.instances.create) Could not fetch resource:
 - The resource 'projects/raft-kv-756/zones/us-central1-c/instances/node2' already exists

✅ VMs created.

🔥 Configuring firewall...
  Firewall rule 'raft-cluster' already exists — skipping.

🔨 Cross-compiling for linux/amd64...
✅ Binaries built: kv-chaos kv-client kv-dashboard kv-store node-agent 

📡 Fetching VM internal IPs (waiting for VMs to be ready)...
  node0 internal IP: 10.128.0.2
  node1 internal IP: 10.128.0.3
  node2 internal IP: 10.128.0.4

🧹 Cleaning up any existing cluster processes and old data...
Connection to 34.55.217.20 closed.
Connection to 34.72.134.22 closed.
Connection to 34.134.90.239 closed.
✅ Cleanup complete.

📦 Uploading binaries to VMs...
  → node0...
kv-chaos                                                                         100% 3498KB   1.3MB/s   00:02    
kv-client                                                                        100%   14MB   3.2MB/s   00:04    
kv-dashboard                                                                     100%   18MB   3.8MB/s   00:04    
kv-store                                                                         100%   17MB   3.1MB/s   00:05    
node-agent                                                                       100% 8619KB   4.1MB/s   00:02    
Connection to 34.55.217.20 closed.
index.html                                                                       100%   30KB 293.8KB/s   00:00    
verify.sh                                                                        100%   15KB 283.6KB/s   00:00    
verify_phase2.sh                                                                 100%   11KB 211.9KB/s   00:00    
verify_phase3.sh                                                                 100%   11KB 199.4KB/s   00:00    
verify_phase4.sh                                                                 100%   13KB 238.1KB/s   00:00    
  → node1...
kv-chaos                                                                         100% 3498KB   6.5MB/s   00:00    
kv-client                                                                        100%   14MB  20.0MB/s   00:00    
kv-dashboard                                                                     100%   18MB  20.1MB/s   00:00    
kv-store                                                                         100%   17MB   9.6MB/s   00:01    
node-agent                                                                       100% 8619KB  17.4MB/s   00:00    
  → node2...
kv-chaos                                                                         100% 3498KB   5.4MB/s   00:00    
kv-client                                                                        100%   14MB  18.6MB/s   00:00    
kv-dashboard                                                                     100%   18MB  18.7MB/s   00:00    
kv-store                                                                         100%   17MB  19.1MB/s   00:00    
node-agent                                                                       100% 8619KB  16.0MB/s   00:00    
✅ Binaries uploaded.

🔑 Setting execute permissions on each VM...
Connection to 34.55.217.20 closed.
Connection to 34.72.134.22 closed.
Connection to 34.134.90.239 closed.
✅ Permissions set.

💾 Creating data directory on each VM...
Connection to 34.55.217.20 closed.
Connection to 34.72.134.22 closed.
Connection to 34.134.90.239 closed.
✅ Data directories ready.

🚀 Starting node-agent on node0 (bootstrap node)...
Connection to 34.55.217.20 closed.
  Waiting for node0 agent to report alive...
  ✅ node0 alive.

🚀 Starting node-agent on node1 and node2...
  Starting node1...
Connection to 34.72.134.22 closed.
  Starting node2...
Connection to 34.134.90.239 closed.
  Waiting for node1 and node2 agents...
  ✅ node1 alive.
  ✅ node2 alive.

📊 Starting kv-dashboard on node0...
Connection to 34.55.217.20 closed.
✅ Dashboard started.

═══════════════════════════════════════════════════════════════
 Cluster Ready
═══════════════════════════════════════════════════════════════
 ✅ node0  internal=10.128.0.2  external=34.55.217.20
           Raft=:12000  gRPC=:50051  Agent=:9000
 ✅ node1  internal=10.128.0.3  external=34.72.134.22
           Raft=:12001  gRPC=:50052  Agent=:9000
 ✅ node2  internal=10.128.0.4  external=34.134.90.239
           Raft=:12002  gRPC=:50053  Agent=:9000

 Dashboard: http://34.55.217.20:8080

 To run Phase 1 tests from VM-0:
   gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify.sh 8080'
═══════════════════════════════════════════════════════════════
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify_phase2.sh 8080'

═══ PRE-FLIGHT ═══
  ℹ️  Cluster: 3 nodes | Leader: node0 | Applied: 7

═══ P1: Minority Partition — SIGSTOP 1 follower ═══
  ℹ️  Pausing follower: node1 (SIGSTOP — hard partition)
  ℹ️  Before — Leader: 7 | Follower: 7
  ℹ️  Pumping 20 writes through leader (:50051)...
  ℹ️  Writes accepted: 20/20
  ℹ️  After  — Leader: 27 (+20) | Follower: 0 (+-7)
  ✅ PASS — P1a: Leader committed +20 entries with follower partitioned (Liveness ✓)
  ✅ PASS — P1b: Frozen follower unreachable (Health timeout → idx=0) — Raft treats it as partitioned ✓
  ✅ PASS — P1c: Paused node status='Dead' — no split-brain ✓
  ℹ️  Resuming node1 (SIGCONT)...

═══ P2: Majority Partition — SIGSTOP the leader ═══
  ℹ️  Current leader: node0 — pausing it (stops heartbeats)
  ℹ️  🌩  Leader node0 is FROZEN — cluster has no heartbeats
  ℹ️  Waiting up to 12s for followers to elect a new leader...
  ℹ️  New leader: 'node1' (elected in ~3s)
  ✅ PASS — P2a: New leader node1 elected in ~3s after leader partition ✓
  ℹ️  Frozen leader node0 appears as: 'Dead' to Health poll
  ✅ PASS — P2b: Frozen node unreachable/stepped-down ('Dead') — no dual-leader ✓
  ℹ️  Resuming frozen leader node0 (SIGCONT — healing partition)...

═══ P3: Heal & Catch-up — formerly frozen node rejoins ═══
  ℹ️  Leader: node1 (idx=28) | Rejoining: node0 (idx=28) | Drift: 0
  ℹ️  Drift small — pumping 20 more writes to widen gap...
  ℹ️  After writes — Leader: 28 | Old leader: 28 | Drift: 0
  ℹ️  Waiting 10s for Raft log replication catch-up...
  ℹ️  node0 index: 28 → 28 (+0 caught up, 0 behind leader)
  ✅ PASS — P3a: Fully caught up to leader (28 = 28) ✓
  ✅ PASS — P3b: Rejoined cleanly as Follower (not causing split-brain) ✓


═══ PHASE 2 RESULTS (with SIGSTOP/SIGCONT partitions) ═══
  ✅ PASS: 7/7  |  ❌ FAIL: 0/7
  All partition guarantees confirmed ✓

Connection to 34.55.217.20 closed.
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify_phase3.sh 8080'

=== PRE-FLIGHT ===
  info Leader: node1

=== R1: Slow Follower -- Does it Hurt Write Speed? ===
  info Baseline: timing 20 writes to leader (:50052)...
  info Baseline: 100405ms for 20 writes
  info Adding 2000ms delay to follower node0...
  info Timing 20 writes to leader with slow follower...
  info With slow follower: 100388ms for 20 writes
  PASS -- R1: Slow follower did not significantly impact writes (100388ms vs baseline 100405ms)

=== R2: Slow Leader -- Does Client Latency Rise? ===
  info Leader: node1 (:50052)
  info Adding 500ms delay to leader node1...
  info Timing 10 writes through chaos proxy (:22001)...
  info Total: 50214ms for 10 writes (avg: 5021ms/write)
  info Leader after slow proxy: node1
  PASS -- R2a: Client latency rose as expected (avg 5021ms >= 400ms threshold)
  PASS -- R2b: Leader remained node1 (no election fired by gRPC delay)

=== R3: Packet Loss -- 50% Drop Rate ===
  info Starting 50% drop proxy on follower node0...
  info Through 50% drop proxy: 0/20 succeeded, 20/20 failed
  FAIL -- R3a: All requests failed (expected ~50% to succeed)
  info Keys readable from leader: 0/20
  PASS -- R3b: All acknowledged writes are durable (0 readable = 0 acknowledged)


=== PHASE 3 RESULTS (Resource & Latency) ===
  PASS: 4/5  |  FAIL: 1/5

Connection to 34.55.217.20 closed.
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % gcloud compute ssh node0 --zone=us-central1-a -- 'bash ~/verify_phase4.sh 8080'

=== PRE-FLIGHT ===
  info Cluster: 3 nodes | Leader: node1

=== D1: Total Wipe -- Full Cluster Restart ===
  info Writing 10 known keys through leader (:50052)...
  info Pre-kill verification: 0/10 keys readable
  info Killing ALL nodes...
  info Verifying all nodes are dead...
  info All nodes confirmed dead
  info Restarting ALL nodes...
  info Waiting for leader election after full restart...
  info New leader: node0
  info Recovered: 0/10 keys after full cluster restart
  FAIL -- D1: No keys recovered after full cluster restart

=== D2: Dirty Restart -- Kill During Active Writes ===
  info Starting background writes...
  info Killing leader node0 mid-write...
  info Writes acknowledged before/during kill: 7
  info LOST acknowledged write: d2_key_1=d2_val_1
  info LOST acknowledged write: d2_key_2=d2_val_2
  info LOST acknowledged write: d2_key_3=d2_val_3
  info LOST acknowledged write: d2_key_4=d2_val_4
  info LOST acknowledged write: d2_key_5=d2_val_5
  info LOST acknowledged write: d2_key_6=d2_val_6
  info LOST acknowledged write: d2_key_7=d2_val_7
  FAIL -- D2: 7 acknowledged writes LOST after leader crash

=== D3: Snapshot Recovery -- Restarted Node Catches Up ===
  info Follower node0 applied_index before kill: 0
  info Killing follower node0...
  info Writing 30 entries while follower is dead...
  info Leader applied_index after writes: 39
  info Restarting follower node0...
  info Waiting 15s for Raft log replay catch-up...
  info node0: 0 -> 39 (+39), leader at 39, gap: 0
  PASS -- D3a: Fully caught up (39 = leader 39) -- log replay successful
  info Keys readable via restarted follower: 0/30
  FAIL -- D3b: No keys readable via restarted follower
  info Note: True snapshot-based recovery requires ~8192+ entries to trigger automatic
  info snapshot compaction. This test verifies log-replay recovery. To test snapshot
  info recovery specifically, lower SnapshotInterval in server/node.go.


=== PHASE 4 RESULTS (Durability) ===
  PASS: 1/4  |  FAIL: 3/4

Connection to 34.55.217.20 closed.
(base) ankushsingh@Ankushs-MacBook-Air4-643 Distributed-RAFT-KV-Storage-prototype % 