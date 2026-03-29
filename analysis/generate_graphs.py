import matplotlib.pyplot as plt
import numpy as np
import os

# Create an output directory for the generated graphs
out_dir = "/Users/ankushsingh/Desktop/CMPT 756/Distributed-RAFT-KV-Storage-prototype/analysis/graphs"
os.makedirs(out_dir, exist_ok=True)

# Set global styles for professional academic charts
plt.style.use('ggplot')
plt.rcParams['font.family'] = 'sans-serif'
plt.rcParams['font.size'] = 12
plt.rcParams['axes.titlesize'] = 14
plt.rcParams['axes.labelsize'] = 12

#####################################################################
# Chart 1: Write Latency Under Network Partitions (Demonstrating CP Quorum)
#####################################################################
scenarios = ['Healthy\n(Baseline)', 'Slow Follower\n(+2000ms Delay)', 'Slow Leader\n(+500ms Delay Proxy)']
latencies_ms = [
    391 / 20.0,    # 19.55 ms/write
    476 / 20.0,    # 23.8 ms/write
    50203 / 10.0   # 5020.3 ms/write
]

fig, ax = plt.subplots(figsize=(8, 6))
bars = ax.bar(scenarios, latencies_ms, color=['#2ca02c', '#1f77b4', '#d62728'], width=0.6)

# Set axis scale to symmetric log to handle the massive disparity
ax.set_yscale('symlog')
ax.set_ylabel('Average Write Latency (ms) - Log Scale')
ax.set_title("Quorum Bypass vs Bottleneck: Client Write Latency\n(Proving N/2 + 1 Consensus Guarantee)")

for bar in bars:
    height = bar.get_height()
    ax.annotate(f'{height:.1f}ms',
                xy=(bar.get_x() + bar.get_width() / 2, height),
                xytext=(0, 3),  
                textcoords="offset points",
                ha='center', va='bottom', weight='bold')

plt.tight_layout()
plt.savefig(os.path.join(out_dir, "latency_quorum_proof.png"), dpi=300)
plt.close()

#####################################################################
# Chart 2: Durability Guarantees Across Failure Modes (D1, D2, D3)
#####################################################################
failure_modes = ['D1: Full Cluster Restart', 'D2: Dirty Active Crash', 'D3: Offline Snapshot Replay']
expected = [10, 7, 30]
recovered = [10, 7, 30]

x = np.arange(len(failure_modes))
width = 0.35

fig, ax = plt.subplots(figsize=(8, 6))
rects1 = ax.bar(x - width/2, expected, width, label='Acknowledged Writes', color='#7f7f7f')
rects2 = ax.bar(x + width/2, recovered, width, label='Recovered Keys', color='#ff7f0e')

ax.set_ylabel('Number of Keys')
ax.set_title('Strict Consistency: Data Durability Across Crash Models\n(Zero Data Loss Proven)')
ax.set_xticks(x)
ax.set_xticklabels(failure_modes)
ax.set_ylim(0, 35)
ax.legend(loc='upper right')

def autolabel(rects):
    for rect in rects:
        height = rect.get_height()
        ax.annotate(f'{height}',
                    xy=(rect.get_x() + rect.get_width() / 2, height),
                    xytext=(0, 3),  # 3 points vertical offset
                    textcoords="offset points",
                    ha='center', va='bottom', weight='bold')

autolabel(rects1)
autolabel(rects2)

plt.tight_layout()
plt.savefig(os.path.join(out_dir, "durability_proof.png"), dpi=300)
plt.close()

#####################################################################
# Chart 3: Mean Time To Recovery (MTTR) / System Availability
#####################################################################
# T = 0 to 5s (Running, 1.0)
# T = 5s (Leader Crash)
# T = 5.0 to 5.5s (Heartbeat Timeout = 500ms)
# T = 5.5 to 6.25s (Election Timeout = 750ms)
# T = 6.25s (New Leader Claimed -> 1.0)
times = [0, 5, 5.001, 5.5, 6.25, 6.251, 10]
availability = [1, 1, 0, 0, 0, 1, 1]

fig, ax = plt.subplots(figsize=(10, 5))
ax.step(times, availability, where='post', color='#9467bd', linewidth=3)

# Shaded regions for breakdown
ax.axvspan(5, 5.5, color='gray', alpha=0.3, label='Heartbeat Timeout Window (500ms)')
ax.axvspan(5.5, 6.25, color='red', alpha=0.3, label='Election Resolving Window (750ms)')

ax.set_xlim(3, 8)
ax.set_ylim(-0.1, 1.1)
ax.set_yticks([0, 1])
ax.set_yticklabels(['Unavailable (Write Blocked)', 'Available (Quorum OK)'])
ax.set_xlabel('Timeline (Seconds)')
ax.set_title('System Availability MTTR Profile (Leader Failover Event)\nTotal Outage = ~1.25s')
ax.legend(loc='lower center', fontsize=10)

# Annotate crash
ax.annotate('Leader Process Killed\n(SIGKILL)', xy=(5, 0), xytext=(3.5, 0.4),
            arrowprops=dict(facecolor='black', shrink=0.05))

# Annotate Recovery
ax.annotate('New Leader Exerts Authority', xy=(6.25, 1), xytext=(6.5, 0.6),
            arrowprops=dict(facecolor='black', shrink=0.05))

plt.tight_layout()
plt.savefig(os.path.join(out_dir, "availability_mttr.png"), dpi=300)
plt.close()

print("Graphs successfully generated at /Users/ankushsingh/Desktop/CMPT 756/Distributed-RAFT-KV-Storage-prototype/analysis/graphs/")
