import matplotlib.pyplot as plt
import numpy as np
import os

# ---------------------------------------------------------------------------
# All numbers from the final GCP run — April 4, 2026 (3-node v1.3)
# Baseline: 3-node cluster, us-central1 cross-zone (~15ms RTT), e2-micro VMs
# ---------------------------------------------------------------------------

out_dir = "/Users/ankushsingh/Desktop/CMPT 756/Distributed-RAFT-KV-Storage-prototype/analysis/graphs"
os.makedirs(out_dir, exist_ok=True)

# Global style
plt.style.use('ggplot')
plt.rcParams['font.family'] = 'sans-serif'
plt.rcParams['font.size'] = 12
plt.rcParams['axes.titlesize'] = 13
plt.rcParams['axes.labelsize'] = 11

#####################################################################
# Chart 1: Throughput Under Fault Injection — Quorum Bypass Proof
#
# Data source: Phase 3, April 4, 2026 GCP run (3-node v1.3)
#   Baseline:       62.9 ops/sec  (15.9 ms/op)
#   Slow follower:  61.3 ops/sec  (16.3 ms/op) — 2000ms netem on one follower
#   Slow leader:     0.6 ops/sec  (1645 ms/op) — 500ms netem on leader NIC
#####################################################################

scenarios = [
    'Baseline\n(Healthy)',
    'Slow Follower\n(+2000ms delay)',
    'Slow Leader\n(+500ms delay)',
]
throughput = [62.9, 61.3, 0.6]
latency_ms = [15.9, 16.3, 1645.0]
colors = ['#2ca02c', '#1f77b4', '#d62728']
retention = ['100%', '97%', '~1%']

fig, ax = plt.subplots(figsize=(8, 5))
bars = ax.bar(scenarios, throughput, color=colors, width=0.55, edgecolor='white', linewidth=0.8)

ax.set_ylabel('Write Throughput (ops/sec)')
ax.set_title(
    'Write Throughput Under Fault Injection (April 4, 2026 — GCP 3-node)\n'
    'Quorum Bypass: slow minority has negligible impact; slow leader is catastrophic'
)
ax.set_ylim(0, 75)

for bar, thr, lat, ret in zip(bars, throughput, latency_ms, retention):
    h = bar.get_height()
    # Throughput label on top of bar
    ax.annotate(
        f'{thr} ops/sec\n({lat} ms/op)\n{ret} retention',
        xy=(bar.get_x() + bar.get_width() / 2, h),
        xytext=(0, 6),
        textcoords='offset points',
        ha='center', va='bottom', fontsize=10, weight='bold'
    )

# Annotate the key insight
ax.annotate(
    'CP trade-off:\nleader is the\ncritical path',
    xy=(2, 0.6), xytext=(1.45, 30),
    arrowprops=dict(facecolor='#d62728', arrowstyle='->', lw=1.5),
    fontsize=9, color='#d62728'
)

plt.tight_layout()
plt.savefig(os.path.join(out_dir, "latency_quorum_proof.png"), dpi=300)
plt.close()

#####################################################################
# Chart 2: Durability — 100% Key Recovery Across Three Crash Models
#
# Data source: Phase 4 (D1, D2, D3), April 4, 2026 GCP run
#   D1: Full cluster wipe + restart — 10 keys pre-written, 10 recovered
#   D2: Dirty leader crash mid-write — 7 writes ACK'd before kill, 7 recovered
#   D3: Follower offline + 30 writes — snapshot install on rejoin, 30 recovered
#####################################################################

failure_modes = ['D1\nFull Cluster\nRestart', 'D2\nDirty Leader\nCrash', 'D3\nSnapshot\nInstall']
acknowledged = [10, 7, 30]
recovered = [10, 7, 30]

x = np.arange(len(failure_modes))
width = 0.35

fig, ax = plt.subplots(figsize=(7, 5))
rects1 = ax.bar(x - width / 2, acknowledged, width, label='Acknowledged Writes', color='#7f7f7f')
rects2 = ax.bar(x + width / 2, recovered, width, label='Recovered Keys', color='#ff7f0e')

ax.set_ylabel('Number of Keys')
ax.set_title('Durability: 100% Key Recovery Across Crash Models\n(BoltDB WAL + Raft snapshots — April 4, 2026)')
ax.set_xticks(x)
ax.set_xticklabels(failure_modes)
ax.set_ylim(0, 38)
ax.legend(loc='upper left')

for rect in list(rects1) + list(rects2):
    h = rect.get_height()
    ax.annotate(
        f'{int(h)}',
        xy=(rect.get_x() + rect.get_width() / 2, h),
        xytext=(0, 3),
        textcoords='offset points',
        ha='center', va='bottom', weight='bold'
    )

# 100% label on each pair
for xi in x:
    ax.text(xi, max(acknowledged[xi], recovered[xi]) + 3.5, '100%\nrecovery',
            ha='center', va='bottom', fontsize=9, color='#ff7f0e', weight='bold')

plt.tight_layout()
plt.savefig(os.path.join(out_dir, "durability_proof.png"), dpi=300)
plt.close()

#####################################################################
# Chart 3: MTTR Profile — Leader Failover Timeline
#
# Data source: Phase 1 L1, Phase 6 N6a — observed on all three GCP runs
#   HeartbeatTimeout = 500ms  (followers detect leader silence)
#   ElectionTimeout  = 750ms  (time to elect new leader after detection)
#   Total MTTR       = 1250ms (matches theoretical minimum on every run)
#####################################################################

times = [0, 5, 5.001, 5.5, 6.25, 6.251, 10]
availability = [1, 1, 0, 0, 0, 1, 1]

fig, ax = plt.subplots(figsize=(9, 4.5))
ax.step(times, availability, where='post', color='#9467bd', linewidth=3)

ax.axvspan(5, 5.5, color='gray', alpha=0.3, label='Heartbeat Timeout (500ms) — followers detect silence')
ax.axvspan(5.5, 6.25, color='#d62728', alpha=0.25, label='Election Window (750ms) — new leader elected')

ax.set_xlim(3, 8.5)
ax.set_ylim(-0.15, 1.2)
ax.set_yticks([0, 1])
ax.set_yticklabels(['Unavailable\n(writes blocked)', 'Available\n(quorum OK)'])
ax.set_xlabel('Timeline (seconds from arbitrary reference)')
ax.set_title('MTTR Profile: Leader Failover (Observed = 1.25s = Theoretical Minimum)\nConsistent across all three GCP runs')
ax.legend(loc='lower right', fontsize=9)

ax.annotate(
    'Leader killed\n(SIGKILL / iptables)',
    xy=(5, 0.05), xytext=(3.2, 0.5),
    arrowprops=dict(facecolor='black', arrowstyle='->', lw=1.4),
    fontsize=9
)
ax.annotate(
    'New leader\nelected',
    xy=(6.25, 0.95), xytext=(6.6, 0.55),
    arrowprops=dict(facecolor='black', arrowstyle='->', lw=1.4),
    fontsize=9
)
ax.text(5.25, 1.1, '500ms', ha='center', fontsize=9, color='gray', weight='bold')
ax.text(5.875, 1.1, '750ms', ha='center', fontsize=9, color='#c00000', weight='bold')

plt.tight_layout()
plt.savefig(os.path.join(out_dir, "availability_mttr.png"), dpi=300)
plt.close()

print("Graphs regenerated (April 4 final-run data):")
print(f"  → {out_dir}/latency_quorum_proof.png  (throughput under fault injection)")
print(f"  → {out_dir}/durability_proof.png       (100% key recovery across crash models)")
print(f"  → {out_dir}/availability_mttr.png      (MTTR leader failover timeline)")
