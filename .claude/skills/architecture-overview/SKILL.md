---
name: architecture-overview
description: Use to orient in this redis-clone before making a change that crosses layers, or to answer "how does the whole thing fit together / how does a request flow / what's the concurrency model / where does X live". The shared mental map the other skills (add-redis-command, run-server, debug-aof) build on. Read this first when unfamiliar with the codebase.
---

# redis-clone architecture

A from-scratch Redis server in Go (`module redis-clone`, `go 1.25`). No external
deps. This is the map; the task skills below assume it.

## Layers (outer → inner)

```
TCP conn ─► cmd/ (main.go, server.go)      connection lifecycle, RESP parse, AOF append
             │
             ▼
        internal/dispatcher/               verb → command-group router
             │
             ▼
        internal/command/                  arg validation + RESP replies (per data type)
             │
             ▼
        internal/store/                    the in-memory data + TTL + eviction (source of truth)

   side systems: internal/aof/ (persistence)   internal/pubsub/ (fan-out)   internal/util.go (RESP read helpers)
```

| Package | Responsibility | Key files |
|---|---|---|
| `cmd` | `main` bootstrap, `Server` lifecycle, per-conn loop, AOF replay/append wiring | `main.go`, `server.go` |
| `internal/dispatcher` | one `switch` mapping verb → `command.HandleXxx` | `dispatcher.go` |
| `internal/command` | stateless handlers `func(ctx *CommandContext, args []string) bool`, RESP writer | `basic/string/list/set/hash/ttl/pubsub/info.go`, `resp_writer.go`, `type.go` |
| `internal/store` | `Store` with `map[string]*Value`, locking, TTL, memory limit + eviction | `store.go` |
| `internal/aof` | append-only log, RESP framing, replay, fsync modes | `aof.go` |
| `internal/pubsub` | `Hub` (channels→subscribers), `Subscriber` (buffered chan) | `hub.go` |
| `internal` | shared RESP read helpers | `util.go` |

`CommandContext` (`command/type.go`) is the seam passed down: `{ Writer, Store,
PubSub, Authenticated }`. Handlers touch state only through it.

## Request flow (one command)

1. `handleConn` (`main.go`) spawns a reader goroutine (`readCommands`) that parses
   **RESP arrays** off the socket (`readRESPCommand` → `internal.ReadBulkString`).
   Anything not starting with `*` is rejected — the wire protocol is RESP **in
   and out**, not plain text (README's "space-based" note is stale).
2. `SUBSCRIBE` forks into a dedicated pub/sub loop (`serveSubscribedConn`); the
   connection stays in "subscribed mode" until it unsubscribes from all channels.
3. Otherwise `dispatcher.Dispatch(ctx, args)` routes the uppercased verb to a
   command group handler, which validates args, calls `Store`/`PubSub`, and
   writes a RESP reply via `ctx.Writer`. It returns `bool` = success.
4. If success **and** `aof.ShouldPersistCommand(verb)`, the command is appended to
   the AOF (after `ArgsForAppend` rewrites relative TTLs to absolute).
5. Reply is flushed. `QUIT` ends the connection.

On startup, `ReplayAOF` replays `appendonly.aof` into the store **before** the
listener opens, restoring prior state.

## Concurrency model — READ THIS before touching shared state

This is **not** a single-threaded event loop (README §11 describes an aspiration,
not the code). Reality:

- **One goroutine per connection** (`Server.trackConnection` → `go handleConn`),
  each with its own reader goroutine. Many run concurrently.
- **Shared state is mutex-guarded, per subsystem:**
  - `store.mu` (`sync.RWMutex`) guards the whole key map. Methods lock at entry;
    internal helpers are the `...Locked` suffixed ones and assume the lock is held.
  - `hub.mu` guards channel→subscriber maps; each `Subscriber` has its own `mu`.
  - `aof.mu` guards the file writer.
- **Background goroutines** touch the same state and must respect the same locks:
  - TTL checker (`StartTTLCheckerContext`, ~1s) deletes expired keys.
  - Eviction checker (`StartEvictionCheckerContext`, ~1s) enforces `MaxMemory`.
  - AOF fsync-every-second worker (`syncEverySecond`).

**Invariant:** never read or mutate `store.data` outside a `store` method holding
`s.mu`. Add new store logic as a method that locks, or as a `...Locked` helper
called from one. The `writeLocked` wrapper additionally gives writes memory-limit
enforcement + rollback — use it for all mutations (see `add-redis-command`).

## The Value model (`store.Value`)

One struct holds every type; `Type` (`StringType/ListType/SetType/HashType`)
selects which field is live (`Str/List/Set/Hash`). Plus per-key metadata:
`ExpiresAt` (TTL, ms; 0 = none), `LastAccessAt`/`AccessCount` (feed LRU/LFU
eviction), `CreatedAt`. Operating on a key of the wrong `Type` returns
`ErrWrongType` → surfaced as `WRONGTYPE` at the command layer.

## Cross-cutting concerns & where they live

| Concern | Where | Note |
|---|---|---|
| Wire protocol (RESP) | `util.go` (read), `command/resp_writer.go` (write) | request + response both RESP |
| Persistence | `aof/` + `main.go` append/replay sites | gated by `ShouldPersistCommand` → `debug-aof` |
| TTL expiry | `store.go` (lazy on access + active checker) | absolute ms timestamps |
| Memory & eviction | `store.go` (`enforceMemoryLimitLocked`, policies) | configured via `.env` → `run-server` |
| Auth | `command` group handlers check `ctx.Authenticated` | currently hard-set `true` in `handleConn` (AUTH is a TODO) |
| Pub/Sub | `pubsub/` + `serveSubscribedConn` in `main.go` | subscriber = buffered chan, fan-out in `Publish` |
| Graceful shutdown | `server.go` `Shutdown` | drain conns → stop workers → flush/close AOF |

## Sibling skills (the "how to act" layer)

- **add-redis-command** — add/extend a command across all layers.
- **run-server** — run + smoke-test with `redis-cli`, config via `.env`.
- **debug-aof** — persistence not working / keys reappearing / replay errors.

## Docs

`README.md` = feature roadmap (some notes outdated, e.g. protocol & §11 threading).
`docs/` has `IMPLEMENTATION_PHASES.md`, `design-pattern.md`, and per-phase
explanations. Trust the code over the docs where they disagree.
