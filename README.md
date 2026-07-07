# Redis Clone

A from-scratch, in-memory Redis server written in Go — no external dependencies.
It speaks the [RESP](https://redis.io/docs/latest/develop/reference/protocol-spec/)
protocol, so you can talk to it with a real `redis-cli`.

> Building it, not just using it? Start with **[`PLAN.md`](PLAN.md)** (roadmap) and
> the `architecture-overview` skill in `.claude/skills/` (how the pieces fit).

## Features

- **RESP protocol** on request and response (TCP, port `6379`).
- **Data types**: strings, lists, sets, hashes — with `WRONGTYPE` checking.
- **TTL / expiry**: passive (on access) + active (background sweeper).
- **Persistence**: append-only file (AOF) with replay on startup and
  configurable fsync (always / every second / never).
- **Memory management**: `MAXMEMORY` limit with LRU / LFU / random eviction policies.
- **Pub/Sub**: `SUBSCRIBE` / `UNSUBSCRIBE` / `PUBLISH` with subscribed-mode connections.
- **Graceful shutdown**: drains connections, stops workers, flushes AOF.

See [`PLAN.md`](PLAN.md) for the full command list and what's done vs. planned.

## Quick start

```bash
make run              # go run ./cmd  (listens on :6379)
```

In another terminal (needs a Redis CLI installed):

```bash
redis-cli -p 6379 PING            # PONG
redis-cli -p 6379 SET k hello
redis-cli -p 6379 GET k           # "hello"
redis-cli -p 6379 SET k v EX 60   # expires in 60s
redis-cli -p 6379 LPUSH mylist a b c
redis-cli -p 6379 LRANGE mylist 0 -1
```

The wire protocol is RESP, so plain `nc`/`telnet` won't work without hand-typing
RESP frames — use `redis-cli`.

## Make targets

| Command | Description |
|---|---|
| `make run` | Run locally (`go run ./cmd`) |
| `make dev` | Run with [Air](https://github.com/air-verse/air) hot-reload |
| `make build` | Build binary to `bin/redis-server` |
| `make test` | Run all tests (`go test ./...`) |
| `make docker-build` | Build the Docker image |
| `make compose-up` / `compose-down` / `compose-logs` | Run via docker-compose |

Run `make help` for the full list.

## Configuration

Config is read from `.env` at startup; **real environment variables override**
`.env`. Set them inline to override per run:

```bash
MAXMEMORY=2097152 MAXMEMORY_POLICY=noeviction go run ./cmd
```

| Variable | Meaning | Default |
|---|---|---|
| `MAXMEMORY` | Memory limit in bytes; `0`/empty disables the limit | `10485760` (`.env`) |
| `MAXMEMORY_POLICY` | `noeviction`, `allkeys-lru`, `volatile-lru`, `allkeys-lfu`, `allkeys-random` | `allkeys-lru` (`.env`) |

State persists to `appendonly.aof` in the working directory and is replayed on
the next start.

## Architecture (at a glance)

Requests flow inward through four layers; AOF and Pub/Sub sit alongside:

```
TCP conn ─► cmd/            connection lifecycle, RESP parse, AOF append/replay
             │
             ▼
        dispatcher/         verb → command-group router
             │
             ▼
        command/            arg validation + RESP replies (per data type)
             │
             ▼
        store/              in-memory data + TTL + eviction (source of truth)

  side systems: aof/ (persistence)   pubsub/ (fan-out)
```

**Concurrency:** one goroutine per connection; shared state is guarded by
per-subsystem mutexes (`store.mu`, `hub.mu`, `aof.mu`) plus background workers for
TTL, eviction, and AOF fsync. (It is *not* a single-threaded event loop.)

Full detail — request flow, invariants, where each concern lives — is in the
**`architecture-overview`** skill under `.claude/skills/`, and per-phase design
notes are in [`docs/`](docs/).

## Project layout

```
cmd/                 main, server lifecycle, per-connection loop
internal/
  dispatcher/        command routing
  command/           command handlers + RESP writer (basic, string, list, set, hash, ttl, pubsub, info)
  store/             key-value store, TTL, eviction
  aof/               append-only-file persistence
  pubsub/            publish/subscribe hub
  util.go            RESP read helpers
docs/                implementation phases & design notes
```

## Testing

```bash
make test            # go test ./...
```

Tests live next to the code (`*_test.go`) and assert raw RESP bytes for command
handlers plus behavior for the store, AOF, and pub/sub.
