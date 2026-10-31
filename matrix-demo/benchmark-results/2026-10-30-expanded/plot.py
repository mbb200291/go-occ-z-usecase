"""Render measured CSV data. No interpolation or invented values."""
import csv
import json
import math
import pathlib
import sys
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.colors import TwoSlopeNorm

root = pathlib.Path(sys.argv[1])
with (root / "summary.csv").open() as f:
    rows = list(csv.DictReader(f))
meta = json.loads((root / "metadata.json").read_text())
ratios = meta["read_parts"]
coverages = meta["coverage"]
cells = [max(1, math.floor(meta["n"]**2*c + .5)) for c in coverages]
logs = [math.log2(float(r["speedup_ratio_of_medians"])) for r in rows]
span = max(1, max(abs(v) for v in logs))
norm = TwoSlopeNorm(vmin=-span, vcenter=0, vmax=span)
for backend, title in [("file", "File targets (one file per cell)"), ("memory", "Memory targets (atomic cells)")]:
    lookup = {(int(r["read_parts"]), int(r["cells"])): r for r in rows if r["backend"] == backend}
    grid = [[math.log2(float(lookup[x, y]["speedup_ratio_of_medians"])) for x in ratios] for y in cells]
    fig, ax = plt.subplots(figsize=(max(8, len(ratios)*.85), max(5, len(coverages)*.65+2.5)), layout="constrained")
    im = ax.imshow(grid, cmap="RdBu", norm=norm, origin="lower", aspect="auto")
    ax.set_xticks(range(len(ratios)), [f"{r}:{10-r}" for r in ratios])
    ax.set_yticks(range(len(coverages)), [f"{float(lookup[ratios[0], c]['actual_coverage']):.1%} ({lookup[ratios[0], c]['cells']})" for c in cells])
    ax.set_xlabel("Read-only requests : XOR requests")
    ax.set_ylabel("Targets per request / all targets (cell count)")
    ax.set_title(f"{title}\nSequential / OCC parallel elapsed time", loc="left", pad=14)
    for yi, c in enumerate(cells):
        for xi, r in enumerate(ratios):
            value = float(lookup[r,c]["speedup_ratio_of_medians"])
            ax.text(xi, yi, f"{value:.2f}", ha="center", va="center", fontsize=9, color="white" if abs(math.log2(value)) > span*.6 else "#17252b")
    bar = fig.colorbar(im, ax=ax, shrink=.82)
    ticks = [-span, -span/2, 0, span/2, span]
    bar.set_ticks(ticks, labels=[f"{2**v:.2g}" for v in ticks])
    bar.set_label("Speedup (log scale; 1 = equal)")
    fig.supxlabel(f"{meta['n']}×{meta['n']} cells · {meta['requests']} requests · {meta['workers']} workers · {meta['repeats']} paired runs; ratio of median times\nBoth modes use OCC + disk journal; warmed targets; >1 is faster. Shared color scale.", fontsize=9)
    for extension in ["png", "svg"]:
        fig.savefig(root / f"{backend}.{extension}", dpi=180)
    plt.close(fig)
    print(root / f"{backend}.png")
