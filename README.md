# go-occ-z use cases

Runnable demos and benchmarks for [go-occ-z](https://github.com/mbb200291/go-occ-z).
Requires Go 1.25 or newer.

| Folder | OCC version | What it runs |
| --- | --- | --- |
| [matrix-demo](matrix-demo) | v0.1.0 | Concurrent XOR matrix updates and file/memory benchmarks |
| [occ-visualizer](occ-visualizer) | v0.1.0-observe.1 | Interactive timeline of real OCC transactions |

Each folder is a separate Go module. Run commands inside that folder; there is
no root module or go.work, so the visualizer's observer version cannot replace
the matrix benchmark's dependency.

```sh
cd matrix-demo
go run ./cmd/matrix-demo -n 4 -blocks 8 -block-size 2 -read-size 4
```

```sh
cd occ-visualizer
go run .
```

Open http://127.0.0.1:8090 for the visualizer.

In either module:

```sh
go test -race -count=1 -timeout=5m ./...
go vet ./...
```

The original demo worktrees were copied with their tests and existing benchmark
artifacts. The OCC implementation itself is consumed from published tags, not
copied into this repository. Original worktrees remain available in the source
repository; this import does not rewrite their history.
