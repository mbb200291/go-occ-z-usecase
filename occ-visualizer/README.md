# Live OCC visualizer

Uses go-occ-z v0.1.0-observe.1 and real disk-backed transactions.

```sh
go run .
# Optional:
go run . -addr 127.0.0.1:8091 -jitter=false
```

Open http://127.0.0.1:8090. No frontend build is needed.

Choose 1–32 parallel requests, target distribution, read-only or increment
commands, and command duration. Add requests while others run.
Playback speed, pause, and replay affect only the view; transactions keep running.

Each request calls Manager.Run. Config.Observer receives read, validation,
conflict, commit, rollback, write, end, and history-purge events from the package.
Each retry has a fresh transaction ID. Rows group attempts by request.

The observer API does not supply a request ID. The demo briefly serializes the
read notification through the start of Command.Execute to bind that ID explicitly,
then releases the gate before command work or storage access. This uses no timing
guesses and does not serialize the whole transaction. All requests must use the
demo command wrapper. Instrumentation affects scheduling; this is not a benchmark.

Retry settings are configured once at startup: 16 retries, 50–500ms exponential
backoff, jitter enabled by default. Use -jitter=false to disable it. Exhausted
conflicts fall back to an exclusive attempt. Request errors are displayed.

Eight uint64 counters occupy independent eight-byte file slots. The adapter uses
ReadAt, WriteAt and Sync. Writes include a 200ms delay to make commits visible.
The sidebar reads disk only when all requests have finished.

Each process creates a fresh temporary directory, printed at startup. Restart to
reset the demo; files remain for inspection. Limits: 32 active requests and 128
total requests per process. Browser tabs share the same session.

R / V / W are logical OCC timestamps; horizontal position is elapsed wall time.
The ongoing and prevTxns views follow stage and purge notifications, not atomic
snapshots of both containers.

```sh
go test -race -count=1 -timeout=2m ./...
go vet ./...
```

The integration test forces a stale-read conflict with a read barrier, verifies
request/attempt grouping, retries, final values, purge events and HTTP validation.
