# Matrix XOR demo

An n × n binary matrix, initially all zero. Concurrent commands read subsets
or XOR local blocks into the matrix. The generated write set always ends at all
ones, regardless of command order.

## Run

From the matrix-demo module directory:

```sh
go run ./cmd/matrix-demo
```

A smaller example:

```sh
go run ./cmd/matrix-demo -n 4 -blocks 8 -block-size 2 \
  -reads 12 -read-size 4 -workers 4 -interval 100ms
```

In an interactive terminal, the matrix redraws in place using ANSI clear/home
sequences. Keep the terminal large enough to fit the matrix. Redirected output
keeps each snapshot as plain text; TERM=dumb also disables redraw. **Every displayed matrix comes from a full-matrix OCC read command.**
Failed reads are retried by OCC; their private results are never displayed.
A busy writer workload can delay snapshots until a read passes validation.

The program creates a fresh temporary directory and prints its path. Files are
kept for inspection. Use `-dir /path/to/empty-directory` to choose the location.
Run each demo in a new process and an empty directory.

| Flag | Default | Meaning |
|---|---:|---|
| `-n` | 8 | Matrix side length, 2–256 |
| `-workers` | 8 | Concurrent command workers |
| `-blocks` | 24 | Random local XOR blocks; correction blocks are added |
| `-block-size` | 2 | Maximum block side, strictly smaller than n |
| `-reads` | 32 | Subset read commands mixed into the workload |
| `-read-size` | 8 | Distinct cells in each subset read, at most n² |
| `-seed` | 42 | Reproducible commands and initial shuffle |
| `-interval` | 200ms | Time between display read requests |
| `-pause` | 15ms | Pause after each worker command; use 0 for timing |
| `-dir` | temporary | Empty output directory |

For n=2, also set `-block-size 1 -read-size 4` or smaller.
Display reads are additional commands, independent of `-reads` and `-read-size`.
The interval is a request cadence, not a guaranteed refresh rate.

## Commands

`commands.json` contains the exact command set. Coordinates are zero-based.
For example, this write flips two cells and leaves one unchanged:

```json
{
  "ID": 7,
  "Kind": "xor",
  "Cells": [
    {"Row": 1, "Col": 2, "Bit": 1},
    {"Row": 1, "Col": 3, "Bit": 0},
    {"Row": 2, "Col": 2, "Bit": 1}
  ]
}
```

A `read` job uses the same coordinate list; its Bit fields are ignored. Read
results appear in `events.jsonl` in the same order as the job's cells.

XOR writes declare both read and write access: they need each cell's old value.
Every target is one cell (`"row,col"`). No write uses a whole-matrix mask.

## Why the result is all ones

The generator creates random local blocks with 0/1 entries, computes their XOR
parity, then adds local correction tiles. Each correction bit is `1 XOR parity`.
Finally, it shuffles all writes and reads together. Corrections are ordinary
writes and do not have to run last.

Each cell receives an odd number of 1s. Zero entries do not change its value.
XOR is commutative, so the expected result is independent of execution order.
Changing the worker count with the same seed and workload flags keeps the same
command set; actual scheduling and snapshots may differ.

## Files and storage

- `initial.csv`: the all-zero starting matrix.
- `matrix.csv`: the live matrix, checked to be all ones at the end.
- `commands.json`: generator parameters and shuffled commands.
- `events.jsonl`: command results, attempt counts, and display snapshots.
- `result.json`: successful command count, attempts, conflicts, snapshots, and elapsed time.
- `.occ/`: OCC's lock, journals, and backups.

CSV has no header. Each row is exactly `2*n` bytes, including its newline.
Cell (row, col) is at offset `2*(row*n+col)`. The adapter writes one ASCII byte
with WriteAt and Sync; separators never move. There is no whole-file write lock.
Directly opening the CSV during execution does not provide an OCC snapshot.
Event order is result arrival order, not an OCC serialization log.

This is a fresh-run demo, not a crash-recovery tool. Do not reuse or overwrite
an interrupted run's directory. Each directory's manager lock lasts until process
exit; the demo keeps cwd fixed while its commands and cleanup run.

## Tests and timing

```sh
go test ./internal/matrixdemo ./cmd/matrix-demo
go test -race ./internal/matrixdemo ./cmd/matrix-demo

# Same command set, different worker counts. Each invocation creates a fresh directory.
go run ./cmd/matrix-demo -workers 1 -seed 42 -pause 0 -interval 1h
go run ./cmd/matrix-demo -workers 8 -seed 42 -pause 0 -interval 1h
```

Tests check seeded parity across sizes and seeds, reversed write order, CSV
offsets, command behavior, and concurrent runs with different read sizes.
They also compare the command files for serial and concurrent runs.

Reported conflicts cover workload commands, excluding display reads. Display
attempts are shown separately with each snapshot. Timing includes durable writes,
retries, display reads, and configured worker pauses. A correct final XOR checks
for lost updates in this workload; it does not prove crash safety or every possible
read-isolation scenario.

## Benchmark grid

See [matrix-bench](../matrix-bench/README.md) for file and memory heatmaps of
parallel OCC speedup across read/write ratios and per-request target coverage.
