# Redis Clone – Implementation Plan

A complete, ordered roadmap for building the Redis clone. This is the **high-level
checklist**; for a phase-by-phase account of *what was actually built and why*,
see [`docs/IMPLEMENTATION_PHASES.md`](docs/IMPLEMENTATION_PHASES.md). For how the
finished pieces fit together, see the `architecture-overview` skill.

Legend: ✅ done · 🟡 partial · ⬜ planned

---

## 1. Connection Layer ✅
- TCP server accepting multiple client connections.
- Connection lifecycle: read → process → write → close.
- One goroutine per connection.

## 2. Basic Commands ✅
- RESP array parsing + a command router/dispatcher.
  - *(Note: the original plan skipped RESP for space-separated input; the
    implementation instead speaks full RESP on the request side.)*
- `PING`, `ECHO`, `SET`, `GET`.

## 3. Authentication 🟡
- Server config for password. ⬜
- Track connection authentication state. 🟡 (`CommandContext.Authenticated`
  exists and write commands check it, but it is hard-set to `true` — `AUTH` is
  not yet implemented.)
- `AUTH` ⬜ · `QUIT` ✅

## 4. Core Key-Value Store ✅
- In-memory `map[string]*Value`.
- Value types: string, list, set, hash.
- Type checking + error messages (WRONGTYPE, missing-key handling).

## 5. TTL (Time To Live) ✅
- `EXPIRE`, `TTL` (+ `PEXPIREAT` for replay-safe absolute expiry).
- Passive expiration: checked on access.
- Active expiration: background goroutine scanning expired keys.

## 6. Data Structures 🟡
### List
- `LPUSH` ✅ · `RPUSH` ✅ · `LRANGE` ✅ · `LPOP` ⬜ · `RPOP` ⬜
### Set
- `SADD` ✅ · `SMEMBERS` ✅ · `SREM` ⬜
### Hash
- `HSET` ✅ · `HGET` ✅ · `HGETALL` ⬜ · `HDEL` ⬜

## 7. Persistence 🟡
### Append-Only File (AOF) ✅
- Append each write command to the AOF (RESP framing).
- Replay AOF at startup to restore state.
- Fsync modes: always / every second / never.
### AOF Rewrite (Compaction) ⬜
- Minimal rewrite of the DB into a new AOF, atomic swap, background run.
### RDB Snapshotting ⬜ (optional, later)

## 8. Eviction & Memory Management ✅
- Config via `.env`; shell env vars override, e.g.
  `MAXMEMORY=2097152 MAXMEMORY_POLICY=noeviction go run ./cmd`.
- `MAXMEMORY=<bytes>`, `MAXMEMORY_POLICY=<policy>`.
- Policies: `noeviction`, `allkeys-lru`, `volatile-lru`, `allkeys-lfu`,
  `allkeys-random`.
- Tracks approximate memory usage + per-key last-access time and access count.
- Background eviction loop keeps usage under the limit.
- Returns OOM when a write cannot fit and nothing can be evicted.

## 9. Pub/Sub ✅
- `SUBSCRIBE`, `UNSUBSCRIBE`, `PUBLISH`.
- Broadcast to subscribers; subscribed-mode connections use goroutines + channels.
- Observer-style hub: `Hub` tracks channels/subscribers; `Subscriber` receives via
  a buffered Go channel; `PUBLISH` fans out and returns the receiver count.

## 10. Graceful Shutdown ✅
- Catch SIGTERM / SIGINT.
- Flush AOF before exit.
- Stop background workers (TTL, eviction; AOF rewrite once it exists).
- Close all client connections cleanly.

## 11. Concurrency model ✅ (revised from original plan)
The original plan aimed for a single-threaded Redis-style event loop:

```
Client A ─┐
Client B ─┼─► event loop (1 thread) ─► execute command
Client C ─┘
```

The implementation instead uses **one goroutine per connection** with shared
state protected by per-subsystem mutexes (`store.mu`, `hub.mu`, `aof.mu`). See
the `architecture-overview` skill for the invariants.

---

## Optional Advanced Features (Future) ⬜
- Replication (MASTER / REPLICA)
- WATCH / UNWATCH (optimistic locking)
- Cluster mode (hash slots)
- Slowlog
- `INFO` command — ✅ already implemented
- `MONITOR` command
- Transactions (`MULTI` / `EXEC` / `DISCARD`)
  - Per-connection transaction queue.
  - Atomic execution of queued commands; handle errors inside the queue.
