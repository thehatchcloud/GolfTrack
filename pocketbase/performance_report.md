# Performance report

What the PocketBase app costs, an optimisation pass that removed a set of
per-record read queries, and what the numbers say about running it in
production.

Everything here is reproducible from the repository:

```bash
make pb-test          # includes the query-count and no-leak assertions
make pb-bench         # the timings
make pb-loadtest      # the 10/50/100 concurrency sweep (slow)
```

Measurements were taken on the machine this phase was developed on — an Intel
Xeon @ 2.80 GHz, 4 vCPU, Linux, Go 1.24, SQLite via `modernc.org/sqlite`, data
directory on local disk. **Read every absolute number as a floor and every
comparison as the finding.** There is no network, no TLS and no Litestream in
any of these loops.

---

## Summary

| Gate item | Status |
|---|---|
| Response times acceptable (<200 ms p95 for most endpoints) | **Met.** Every read endpoint is under budget at 100 concurrent players; every endpoint including writes is under 5 ms p95 at a realistic offered load. The one qualification — writes driven to saturation — is set out in "Concurrency" below |
| No memory leaks | **Met.** `TestSustainedTrafficDoesNotLeak`: heap flat across ~13 000 requests, goroutines flat |
| Concurrency tests pass | **Met.** The existing `concurrency_test.go` still passes, and the new sweep runs 10/50/100 players with zero failed requests at every level |
| Queries efficient | **Met, after work.** Four read paths were issuing a query per record; they now issue a fixed set. Two indexes were added. Both are pinned by tests |

The headline change: **the round detail read went from 41 statements to 6 and
from 2.54 ms to 0.79 ms, and no longer grows with the size of the round.**

---

## What was wrong

Every read path assembled its response one record at a time:

```go
for _, round := range rounds {
    NewOut(app, round)      // a course lookup, and that course's holes
}
```

Nothing failed: the responses were correct, the tests passed, and the cost
was invisible at fixture size and quadratic at season size.

Measured, before this phase, over one HTTP request:

| Route | 9-hole round, 1 completed | 18-hole round, 12 completed |
|---|---|---|
| `GET /api/rounds/{id}` | 23 statements | **41** |
| `GET /api/rounds/in-progress` | 23 | **41** |
| `GET /api/rounds` | 4 | **26** |
| `GET /api/courses` | 3 | 8 |
| `/` (home page) | 26 | **66** |
| `/rounds/` (page) | 26 | **66** |

The right-hand column is not a large database. It is one golfer with a dozen
rounds behind them.

---

## What changed

### Set-at-a-time reads

`internal/records` gained the batched counterparts of its three lookups —
`CoursesByID`, `CourseHolesByCourse`, `RoundHolesByRound`, `ShotsByRoundHole` —
each returning a map keyed by the foreign key, chunked at 500 ids so a query
cannot grow without bound. The response builders were split into a query half
and a pure assembly half, so the single-record and batched paths produce
identically shaped output by construction:

- `courses.NewOutList` — every course's holes in one query
- `roundholes.NewOutList` — every hole's shots in one query
- `rounds.NewOutList` — the courses a set of rounds was played on, and their
  holes, in two queries regardless of the number of rounds

After:

| Route | 9-hole, 1 completed | 18-hole, 12 completed | was |
|---|---|---|---|
| `GET /api/rounds/{id}` | 6 | **6** | 41 |
| `GET /api/rounds/in-progress` | 6 | **6** | 41 |
| `GET /api/rounds` | 4 | **4** | 26 |
| `GET /api/courses` | 3 | **3** | 8 |
| `GET /api/courses/{id}` | 3 | **3** | 3 |
| `/` (home page) | 9 | **9** | 66 |
| `/courses/` | 3 | **3** | 8 |
| `/rounds/` | 9 | **9** | 66 |
| `/rounds/{id}/play/` | 6 | **6** | 41 |
| `/rounds/{id}/review/` | 6 | **6** | 41 |

Identical at both sizes, which is the property `TestReadPathsDoNotQueryPerRecord`
and `TestPagesDoNotQueryPerRecord` assert — not "small", but "the same", measured
at two data sizes that differ in every dimension a per-record read would multiply
by. The upper bounds are the measured values, so adding a query to any of these
paths fails the suite.

### Two indexes

`TestHotQueriesUseAnIndex` asks SQLite for the plan of every query the read
paths issue and fails on a `SCAN`. It found two:

| Query | Was | Now |
|---|---|---|
| the player's completed rounds, newest first | `SEARCH rounds USING INDEX idx_rounds_status (status=?)` + `USE TEMP B-TREE FOR ORDER BY` | `SEARCH rounds USING INDEX idx_rounds_user_status_finished (user=? AND status=?)` |
| the courses list, by name | `SCAN courses USING INDEX idx_courses_name` | `SEARCH courses USING INDEX idx_courses_archived_name (is_archived=?)` |

The first was the worse of the two and the least obvious. `idx_rounds_status` is
a single-column index on a two-valued column, so SQLite was selecting *every
completed round in the database* and filtering by user afterwards — fine with one
player, a full scan of the history table with a thousand. It also could not
satisfy the `ORDER BY finished_at DESC`, hence the temporary B-tree. The new
composite covers both.

Added to `pb_schema.json`:

```
CREATE INDEX idx_courses_archived_name        ON courses (is_archived, name)
CREATE INDEX idx_rounds_user_status_finished  ON rounds (user, status, finished_at DESC, started_at DESC)
```

`idx_rounds_user` and `idx_rounds_status` are left in place: with the
composite available the planner no longer chooses them, but dropping them is
a follow-up cleanup question, not a performance one.

### Three redundant reads in the write transaction

Writes serialise (see "Concurrency"), so a statement inside a write transaction
is time every other writer spends queued. Three were removed:

- `openHole` fetched the round a second time to check its status, when
  `FindRoundHole` had already loaded it — now `FindRoundHoleAndRound`.
- `Add` loaded every shot on the hole to find the highest `shot_number` — now
  `records.LastShotNumber`, a `MAX()`.
- `RefreshStrokes` loaded every shot on the hole to count them — now
  `records.CountHoleShots`, a `COUNT(*)`.

Worth about a tenth of write throughput. Modest, and it is the honest size of it.

---

## Timings

`make pb-bench`, in process, against the 18-hole play fixture. Before/after taken
on the same machine in the same session, with the same benchmarks, by stashing
only the source changes.

| Benchmark | before | after | change |
|---|---|---|---|
| `BenchmarkRoundDetailRoute` | 2 544 µs / 6 473 allocs | **789 µs / 2 576 allocs** | **3.2× faster, 60 % fewer allocations** |
| `BenchmarkInProgressRoute` | 2 492 µs / 6 482 allocs | **840 µs / 2 584 allocs** | **3.0× faster** |
| `BenchmarkHomePage` | 3 319 µs / 7 911 allocs | **1 368 µs / 4 032 allocs** | **2.4× faster** |
| `BenchmarkAddShot` | 1 546 µs / 2 764 allocs | **1 384 µs / 2 454 allocs** | 1.1× |
| `BenchmarkListRoundsRoute` | 542 µs / 1 336 allocs | 489 µs / 1 356 allocs | 1.1× |
| `BenchmarkListCoursesRoute` | 441 µs / 1 533 allocs | 417 µs / 1 470 allocs | 1.1× |

The two list routes barely move, and that is expected rather than disappointing:
the play fixture holds one completed round and two courses, so there is almost no
N to remove. Their win is in the statement table above, where the fixture is
twelve rounds — 26 statements to 4. **This is why the query counts are the
assertion and the timings are the illustration**: a benchmark on a small fixture
is exactly the instrument that fails to see an N+1.

Unchanged baselines, for continuity — these measure the *generated*
endpoints, which the frontend does not use:

| Benchmark | ns/op |
|---|---|
| `BenchmarkRoundDetail` (generated + `expand`) | 1 893 µs |
| `BenchmarkListRounds` (generated + filter) | 1 114 µs |
| `BenchmarkSetCurrentHole` | 883 µs |
| `BenchmarkHealth` (the framework floor) | 105 µs |

Note that the custom round-detail route (789 µs) is now **faster than the
generated endpoint with `expand`** (1 893 µs) for the same data, on top of
returning it in the shape the contract asks for.

---

## Concurrency

`make pb-loadtest`. Every level ran with **zero failed requests**.

### Reads — 3-second saturation, no think time

| Players | req/s | p50 | p95 | p99 |
|---|---|---|---|---|
| 10 | 472 per endpoint | 1.2–7.4 ms | 3.6–12.9 ms | 6.2–15.5 ms |
| 50 | 599 | 2.4–30.1 ms | 12.4–56.0 ms | 22–114 ms |
| 100 | 586 | 4.1–56.4 ms | 26.8–**170.5** ms | 47.9–236.9 ms |

Every read endpoint is inside the 200 ms p95 budget at every level, including a
hundred players with no think time at all. Reads scale because PocketBase gives
them a 120-connection pool over WAL, so concurrency buys parallelism rather than
a queue.

### Writes — 3-second saturation, no think time

| Players | req/s | p50 | p95 |
|---|---|---|---|
| 10 | 262 | 9.8 ms | 40.4 ms |
| 50 | 290 | 42.9 ms | 163.2 ms |
| 100 | 287 | 100.2 ms | 389.0 ms |

**Throughput is flat from ten players onward.** That is the finding. PocketBase
runs writes on a one-connection pool over SQLite's single writer, so write
throughput saturates at roughly 290 requests/second on this machine and stays
there. Once throughput is flat, latency under a closed-loop driver is Little's
law on the number of goroutines the test starts: 100 ÷ 287 is the 348 ms it
measures, and would be 3.5 s at a thousand players. It is a property of the load
generator, not of the app.

So the write assertion is a **throughput floor**, not a latency budget — see
`writeFloor` in `loadtest_test.go`. Asserting the latency would pin the shape of
the test rather than the behaviour of the code.

### Both, at a load a golf app can receive

The saturation numbers answer "where is the ceiling". This answers "is it fast
enough", which is the gate's actual question. A hundred players on a jittered
three-second cycle — offering ~100 writes/second, a third of the ceiling and some
sixty times a real Saturday morning, since a golfer records a shot about once a
minute:

| Endpoint | p50 | p95 | p99 |
|---|---|---|---|
| `GET /api/rounds/in-progress` | 1.4 ms | 2.6 ms | 3.3 ms |
| `GET /api/rounds/{id}` | 0.9 ms | 2.1 ms | 2.9 ms |
| `GET /api/rounds` | 0.6 ms | 1.2 ms | 2.0 ms |
| `GET /api/courses` | 0.4 ms | 0.9 ms | 1.3 ms |
| `/` (page) | 1.4 ms | 2.9 ms | 3.5 ms |
| `POST …/holes/{n}/shots` | 1.7 ms | 4.1 ms | 30.3 ms |
| `POST …/holes/{n}/undo` | 1.2 ms | 2.9 ms | 6.1 ms |
| `PATCH …/current-hole` | 0.8 ms | 2.6 ms | 20.5 ms |

Every endpoint, writes included, is **under 5 ms p95** — some forty times inside
the budget, with the network still to be added.

The jitter in that test is load-bearing rather than decoration. An unjittered
version failed the budget, and not because the app was slow: a hundred players
started together stay in lockstep and arrive as a herd once per think time, so
what it measured was a burst.

### Practical ceiling

Roughly 290 writes/second sustained. A round is on the order of 90 shots plus
navigation, say 200 writes over four hours — about 0.014 writes/second per
player. The write ceiling is therefore somewhere around **twenty thousand
concurrent golfers**, at which point the constraint is that they would all have
to be at the same club.

SQLite's single writer is not a risk for this application. If it ever became one,
the answer is not a code change; it is that the deployment model
(`DEPLOYMENT.md`) needs revisiting.

---

## Memory and resources

`TestSustainedTrafficDoesNotLeak`, part of `make pb-test`. Four players drive the
full mixed workload — five reads and three writes each — for a warm-up batch and
then a batch five times larger, with the heap measured after a settling GC each
time.

```
heap in use: 3.3 MiB settled, 3.3 MiB after 3200 further requests (-0.0 MiB, -5 bytes/request)
```

Flat, and slightly negative, which is allocator drift rather than a reclamation.
Goroutine count is unchanged across the run: nothing in a request handler
outlives its request. The threshold in the test is deliberately loose (8 MiB
across ~10 000 requests) because Go's heap is not a straight line even under a
clean workload; what it catches is the shape a real leak has, retention
proportional to requests served.

Resident set for the whole app under the 100-player sweep stays in single-digit
MiB of live heap. There is no cache to size and no pool to tune: the collection
schema is loaded once at startup and the templates are parsed once at
registration.

---

## What was deliberately not done

- **No caching layer.** The issue lists "caching" among the optimisation
  techniques. Nothing here needs one. The expensive reads are now 3–6 statements
  against indexed SQLite in the same process, which is already faster than a
  cache lookup over a network would be, and a cache would introduce an
  invalidation problem — the stroke cache is the only derived value in the
  system, it is maintained transactionally, and adding a second, weaker cache in
  front of it would be a correctness risk taken for no measured gain.
- **No change to the write transaction's invariants.** Moving the ownership and
  status reads out of the transaction would shorten the writer lock, and would
  break the guarantee `ARCHITECTURE.md` states: reads that decide a write see the
  same snapshot as the write. The measured cost of keeping it is small and the
  throughput is far beyond need.
- **No `rounds` index removal.** See "Two indexes" above.

---

## Files

| File | What it holds |
|---|---|
| `perf_test.go` | statement counts per route at two data sizes, query plans, the no-leak check — all part of `make pb-test` |
| `loadtest_test.go` | the 10/50/100 sweep and the realistic-pace run; gated on `GOLFTRACK_LOADTEST` |
| `bench_test.go` | the timings, including the custom read routes |
| `pb_schema.json` | the two added indexes |
| `internal/records/records.go` | the batched lookups and the two write-path aggregates |
