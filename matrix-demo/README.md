# Matrix demo and benchmarks

Uses go-occ-z v0.1.0.

- [Animated matrix demo](cmd/matrix-demo/README.md)
- [File/memory benchmark grid](cmd/matrix-bench/README.md)
- [Historical benchmark results](benchmark-results)

Run from this directory:

```sh
go run ./cmd/matrix-demo -n 4 -blocks 8 -block-size 2 -read-size 4
go run ./cmd/matrix-bench -n 4 -requests 10 -workers 4 -read-parts 9 -coverage 0.125 -repeats 1 -warmups 0 -work-delay 10ms -plot=false -out /tmp/occ-bench-smoke
```

Choose a new output directory for each benchmark, or use its documented resume
option. Plotting is optional and requires Python 3 with Matplotlib.

The files in benchmark-results and matrix-bench-results predate this migration.
Their metadata records the original environment and paths. They are historical
measurements, not new measurements of v0.1.0.
