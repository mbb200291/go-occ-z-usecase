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
overlap_mode = "actual_overlap" in meta
score_mode = meta.get("heatmap_metric") == "normalized_time_difference"
def metric(r):
    serial = float(r["sequential_median_seconds"])
    parallel = float(r["occ_median_seconds"])
    return (serial-parallel)/(serial+parallel) if score_mode else math.log2(float(r["speedup_ratio_of_medians"]))

logs = [math.log2(float(r["speedup_ratio_of_medians"])) for r in rows]
span = max(1, max(abs(v) for v in logs))
if score_mode:
    span = 1
norm = TwoSlopeNorm(vmin=-span, vcenter=0, vmax=span)
for backend, title in [("file", "File targets (one file per cell)"), ("memory", "Memory targets (atomic cells)")]:
    lookup = {(int(r["read_parts"]), int(r["cells"])): r for r in rows if r["backend"] == backend}
    grid = [[metric(lookup[x, y]) for x in ratios] for y in cells]
    fig, ax = plt.subplots(figsize=(max(8, len(ratios)*.85), max(5, len(coverages)*.65+2.5)), layout="constrained")
    im = ax.imshow(grid, cmap="RdBu", norm=norm, origin="lower", aspect="auto")
    ax.set_xticks(range(len(ratios)), [f"{r}:{10-r}" for r in ratios])
    ax.set_yticks(range(len(coverages)), [f"{float(lookup[ratios[0], c]['actual_coverage']):.1%} ({lookup[ratios[0], c]['cells']})" for c in cells])
    if overlap_mode:
        ax.set_yticks(range(len(cells)), [f"{p:.1%} ({c} targets)" for p,c in zip(meta["actual_overlap"], cells)])
    ax.set_xlabel("Read-only requests : XOR requests")
    ax.set_ylabel("Targets per request / all targets (cell count)")
    subtitle = "(Sequential time − parallel time) / (Sequential time + parallel time)" if score_mode else "Sequential / OCC parallel elapsed time"
    if overlap_mode:
        ax.set_ylabel("Theoretical overlap with any other request")
    ax.set_title(f"{title}\n{subtitle}", loc="left", pad=14)
    for yi, c in enumerate(cells):
        for xi, r in enumerate(ratios):
            value = metric(lookup[r,c]) if score_mode else float(lookup[r,c]["speedup_ratio_of_medians"])
            ax.text(xi, yi, f"{value:.2f}", ha="center", va="center", fontsize=9, color="white" if abs(grid[yi][xi]) > span*.6 else "#17252b")
    bar = fig.colorbar(im, ax=ax, shrink=.82)
    ticks = [-span, -span/2, 0, span/2, span]
    bar.set_ticks(ticks, labels=[f"{v:.2g}" if score_mode else f"{2**v:.2g}" for v in ticks])
    bar.set_label("Normalized time difference; 0 = equal" if score_mode else "Speedup (log scale; 1 = equal)")
    detail = "positive = faster; theoretical target overlap, not conflict rate" if score_mode else ">1 = faster; shared log color scale"
    fig.supxlabel(f"{meta['n']}×{meta['n']} targets · {meta['requests']} requests · {meta['workers']} workers · {meta['repeats']} paired runs · delay {meta.get('work_delay','0s')}\nBoth modes use OCC + disk journal; warmed targets. Median times.\n{detail}.", fontsize=9)
    for extension in ["png", "svg"]:
        fig.savefig(root / f"{backend}.{extension}", dpi=180, bbox_inches="tight")
    plt.close(fig)
    print(root / f"{backend}.png")
