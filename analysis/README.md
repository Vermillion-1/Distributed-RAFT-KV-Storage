# Analysis

Reproducible sources for the figures and numbers reported in
[`results_analysis.md`](results_analysis.md) and on the
[documentation site](https://vermillion-1.github.io/Distributed-RAFT-KV-Storage/).

| File | Contents |
|------|----------|
| `results_analysis.md` | Full write-up: per-phase results, MTTR analysis, the two fault-injection bugs, and interpretation |
| `analysis_notebook.ipynb` | Experimental analysis across all three GCP runs (Mar 29, Apr 1, Apr 4, 2026) |
| `generate_graphs.py` | Regenerates every figure in `graphs/` |
| `graphs/` | Rendered figures used in the docs |

## Reproducing the figures

```bash
pip install matplotlib numpy
python analysis/generate_graphs.py
```

Writes `availability_mttr.png`, `durability_proof.png` and `latency_quorum_proof.png` into
`analysis/graphs/`.

## On the data

All values are hard-coded from real GCP test runs rather than regenerated at plot time — the
scripts visualise measurements, they do not produce them. Every number traces to a specific phase
and run date, cited inline in `results_analysis.md`.

Headline run: **3-node v1.3, April 4 2026**, `us-central1` cross-zone (~15 ms RTT), `e2-micro` VMs.
