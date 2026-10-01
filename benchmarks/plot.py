"""Render committed raw benchmark data; requires matplotlib."""
import json
from pathlib import Path
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
plt.rcParams["svg.fonttype"] = "none"
plt.rcParams["svg.hashsalt"] = "stratadb"

root = Path(__file__).parent
rows = [json.loads(line) for line in (root / "results.jsonl").read_text().splitlines()]
fig, axes = plt.subplots(1, 2, figsize=(12, 4.8), layout="constrained", sharey=True)
colors = {"stratadb": "#118b80", "bbolt": "#536ba8"}
for ax, sync in zip(axes, [True, False]):
    for j, engine in enumerate(colors):
        data = [next(r for r in rows if r["engine"] == engine and r["sync"] == sync and r["workload"] == w) for w in "ABCW"]
        ax.bar([i + (j - 0.5) * .36 for i in range(4)], [r["ops_per_second"] for r in data], width=.36, label=engine, color=colors[engine])
    ax.set_xticks(range(4), ["A · 50% reads", "B · 95% reads", "C · reads", "W · inserts"])
    ax.set_yscale("log")
    ax.set_ylabel("Operations / second · log scale")
    ax.set_title("Sync per write" if sync else "No per-write sync · weaker durability")
    ax.spines[["top", "right"]].set_visible(False)
    ax.grid(axis="y", alpha=.15)
    ax.set_axisbelow(True)
    ax.legend(frameon=False)
fig.suptitle("StrataDB vs bbolt · single-client local sample", fontsize=16)
fig.savefig(root / "throughput.svg", metadata={"Date": None})

svg = root / "throughput.svg"
svg.write_text("\n".join(line.rstrip() for line in svg.read_text().splitlines()) + "\n")
