# Redis Clone – Detailed Implementation Phases

This document describes each implementation phase in depth: the design decisions made, how the code is structured, and what is currently done vs. still planned.

---

## Phase 1 · Connection Layer ✅

**Goal:** Accept TCP connections and read/write RESP-encoded data.

### What was built

The entry point lives in [`cmd/main.go`](./cmd/main.go). It binds a TCP listener on port `6379` and spawns a goroutine for each accepted connection:

```go
ln, err := net.Listen("tcp", ":6379")
// ...
for {
    conn, err := ln.Accept()
    go handleConn(conn, store, aofFile)
}
```

Each connection is handled by `handleConn`, which wraps the raw `net.Conn` in a `bufio.Reader` / `bufio.Writer` for efficient I/O.

### RESP parsing

Rather than a standalone parser package, RESP parsing is handled by two helper functions in [`internal/util.go`](./internal/util.go):

| Function | Description |
|---|---|
| `ReadLine(r)` | Reads one `\r\n`-terminated line and strips the terminator |
| `ReadBulkString(r)` | Reads a `$<len>\r\n<data>\r\n` bulk string |

The main loop in `handleConn` reads an array header (`*<n>\r\n`), then reads `n` bulk strings to form a command argument slice. Only RESP arrays are accepted; inline commands return an error.

### RESP writing

[`internal/command/resp_writer.go`](./internal/command/resp_writer.go) wraps a `bufio.Writer` and exposes typed write methods:

```
WriteError(msg)         → -msg\r\n
WriteSimpleString(msg)  → +msg\r\n
WriteBulkString(val)    → $len\r\nval\r\n
WriteNull()             → $-1\r\n
WriteInteger(val)       → :val\r\n
WriteArrayHeader(n)     → *n\r\n
```

Each write flushes immediately so partial responses are never left in the buffer.

---

## Phase 2 · Basic Commands ✅

**Goal:** Route incoming commands to handler functions.

### Dispatcher

[`internal/dispatcher/dispatcher.go`](./internal/dispatcher/dispatcher.go) implements a single `Dispatch(ctx, args)` function with a `switch` on the uppercased command name. Commands are grouped into handler sets:

```
PING / ECHO / QUIT      → command.HandlePing / HandleEcho / HandleQuit
SET / GET / DEL         → command.HandleStringCommands
LPUSH / RPUSH / LRANGE  → command.HandleListCommands
SADD / SMEMBERS         → command.HandleSetCommands
HSET / HGET             → command.HandleHashCommands
EXPIRE / TTL            → command.HandleTTLCommands
```

`Dispatch` returns `bool` — `true` on success, `false` on unknown command. This return value is used by `handleConn` to decide whether to persist the command to the AOF file.

### CommandContext

Every handler receives a `*command.CommandContext` (defined in [`internal/command/type.go`](./internal/command/type.go)):

```go
type CommandContext struct {
    Writer        *RespWriter
    Store         *store.Store
    Authenticated bool
}
```

This keeps handlers decoupled from both the transport layer and the store implementation.

### Basic command handlers ([`internal/command/basic.go`](./internal/command/basic.go))

| Command | Behaviour |
|---|---|
| `PING [msg]` | Returns `PONG` or the given message as a simple string |
| `ECHO <msg>` | Returns the message as a bulk string |
| `QUIT` | Returns `+OK` and the connection loop exits |

---

## Phase 3 · Authentication ⚠️ (Partial)

**Goal:** Require clients to authenticate before issuing write commands.

### Current state

`CommandContext.Authenticated` is always set to `true` in `handleConn` (marked with a `TODO` comment). All command handlers check this flag and return `NOAUTH Authentication required` if `false`, so the guard logic is in place. What is missing:

- A server-side password configuration (config file or env var).
- The `AUTH <password>` command implementation.
- Setting `Authenticated = false` by default and flipping it only after a successful `AUTH`.

### Design intent

When implemented, `AUTH` would be handled before dispatching to the main command router. On a per-connection basis, the `AOFCommandContext` wrapper (which embeds `CommandContext`) would track auth state.

---

## Phase 4 · Core Key-Value Store ✅

**Goal:** An in-memory store with multiple value types and concurrency safety.

### Data model ([`internal/store/store.go`](./internal/store/store.go))

All values share a single `Value` struct tagged with a `ValueType` discriminant:

```go
type Value struct {
    Type      ValueType   // StringType | ListType | SetType | HashType
    Str       []byte
    List      [][]byte
    Set       map[string]struct{}
    Hash      map[string][]byte
    ExpiresAt int64       // Unix milliseconds; 0 = no expiry
}
```

The store itself is a `map[string]*Value` protected by a `sync.RWMutex`. Read-only operations (`Get`, `LRange`, `SMembers`, `HGet`) use `RLock`; mutating operations use the exclusive `Lock`.

### Type safety

Each store method checks `v.Type` before operating and silently no-ops (or returns a zero value) if the type doesn't match. Commands currently do not return the `WRONGTYPE` Redis error to the client — that is an area for improvement.

### String commands ([`internal/command/string.go`](./internal/command/string.go))

| Command | Notes |
|---|---|
| `SET key value [EX sec \| PX ms]` | Supports `EX` (seconds) and `PX` (milliseconds) TTL options |
| `GET key` | Returns `$-1` (null bulk) for missing or expired keys |
| `DEL key [key ...]` | Returns the count of actually deleted keys |

---

## Phase 5 · TTL (Time To Live) ✅

**Goal:** Keys can expire automatically.

### Passive expiration

`Store.Get` checks `ExpiresAt` on every read. If the current time (in Unix milliseconds) is past the deadline, the key is deleted and `nil, false` is returned. This means expired keys are cleaned up lazily on access at zero extra cost.

### Active expiration ([`internal/store/store.go`](./internal/store/store.go) – `StartTTLChecker`)

A background goroutine is started once at server boot:

```go
store.StartTTLChecker(time.Second)
```

Every second it acquires the write lock, iterates over all keys, and deletes any whose `ExpiresAt` has passed. This prevents unbounded memory growth from keys that are never read again after expiry.

### TTL commands

`EXPIRE key seconds` — sets `ExpiresAt = now + seconds*1000 ms`. Returns `1` if the key exists, `0` if not.

`TTL key` — returns the remaining seconds. Returns `-1` if no expiry is set, `-2` if the key doesn't exist (Redis convention).

> **Note:** `EXPIREAT`, `PEXPIRE`, `PTTL`, and `PERSIST` are not yet implemented.

---

## Phase 6 · Data Structures ✅

### List commands ([`internal/command/list.go`](./internal/command/list.go))

Lists are stored as `[][]byte` slices. Head insertions (`LPUSH`) prepend using Go's slice expansion:

```go
v.List = append([][]byte{val}, v.List...)
```

| Command | Notes |
|---|---|
| `LPUSH key val [val ...]` | Inserts each value at the head; returns new length |
| `RPUSH key val [val ...]` | Appends each value at the tail; returns new length |
| `LRANGE key start stop` | Supports negative indices (Python-style); returns an array |

> **Not yet implemented:** `LPOP`, `RPOP`, `LLEN`, `LINDEX`, `LSET`.

### Set commands ([`internal/command/set.go`](./internal/command/set.go))

Sets are stored as `map[string]struct{}` for O(1) membership checks.

| Command | Notes |
|---|---|
| `SADD key member [member ...]` | Returns count of newly added members |
| `SMEMBERS key` | Returns all members as an unordered array |

> **Not yet implemented:** `SREM`, `SISMEMBER`, `SCARD`, `SINTER`, `SUNION`, `SDIFF`.

### Hash commands ([`internal/command/hash.go`](./internal/command/hash.go))

Hashes are stored as `map[string][]byte`.

| Command | Notes |
|---|---|
| `HSET key field value` | Returns `1` if a new field was created, `0` if updated |
| `HGET key field` | Returns `$-1` if key or field is missing |

> **Not yet implemented:** `HDEL`, `HGETALL`, `HKEYS`, `HVALS`, `HEXISTS`, `HMSET`.

---

## Phase 7 · Persistence (AOF) ✅

**Goal:** Survive restarts by replaying write commands from an append-only log.

### AOF file format ([`internal/aof/aof.go`](./internal/aof/aof.go))

Commands are serialised as standard RESP arrays and appended to `appendonly.aof`. This means the AOF file is compatible with any RESP parser. Example entry for `SET foo bar`:

```
*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n
```

### Fsync modes

Three modes control durability vs. performance:

| Mode | Behaviour |
|---|---|
| `FsyncAlways` | `fsync` after every single `Append` call — safest, slowest |
| `FsyncEverySec` | Background goroutine flushes and syncs once per second (default) |
| `FsyncNever` | OS decides when to flush — fastest, least durable |

The server starts with `FsyncEverySec` (one second of potential data loss on crash).

### Selective persistence

Not all commands need to be logged. `ShouldPersistCommand` whitelists only write commands:

```
SET, DEL, LPUSH, RPUSH, SADD, HSET, EXPIRE
```

Read commands (`GET`, `TTL`, `PING`, etc.) are never written to the AOF.

### Replay on startup

`ReplayAOF` in `main.go` replays the log before the server starts accepting connections. It creates a silent `CommandContext` (writing to `io.Discard`) and re-dispatches every persisted command through the normal dispatcher, which rebuilds in-memory state.

---

## Phase 8 · Eviction & Memory Management ✅

**Status:** Implemented.

**What was added:**
- `store.Config` with `MaxMemory` bytes and `EvictionPolicy`.
- Environment config in `main.go`: `MAXMEMORY` and `MAXMEMORY_POLICY`.
- Approximate memory tracking for strings, lists, sets, hashes, keys, and per-value overhead.
- Access metadata per key (`LastAccessAt`, `AccessCount`) for LRU/LFU approximation.
- Background eviction loop via `StartEvictionChecker`.
- Supported policies: `allkeys-lru`, `volatile-lru`, `allkeys-lfu`, `allkeys-random`, `noeviction`.
- Redis-style OOM errors for writes that cannot fit under the configured memory limit.

---

## Phase 9 · Pub/Sub ❌

**Status:** Not yet implemented.
#### thử cả Observe design pattern và go routine, channel

**Plan:**
- Maintain a `map[channel][]subscriber` where each subscriber is a `chan []byte`.
- `SUBSCRIBE channel` blocks the connection, sending messages as they arrive.
- `PUBLISH channel message` iterates the subscriber list and sends to each channel.
- Unsubscribing removes the subscriber entry and closes its channel.

---

## Phase 10 · Graceful Shutdown ❌

**Status:** Not yet implemented.

**Plan:**
- Use `signal.NotifyContext` or `signal.Notify` to catch `SIGTERM` / `SIGINT`.
- On signal: stop accepting new connections, flush and close the AOF file, signal background goroutines (TTL checker, AOF fsync loop) to stop via a shared context or quit channel, then drain active connections.

---

## Advanced / Future Features

| Feature | Notes |
|---|---|
| **Replication** | MASTER/REPLICA handshake, partial sync via replication offset |
| **WATCH / UNWATCH** | Optimistic locking for transactions |
| **Cluster mode** | Hash-slot sharding across multiple nodes |
| **Slowlog** | Track commands exceeding a latency threshold |
| **INFO** | Return server stats, memory, replication info |
| **MONITOR** | Stream all received commands to a debug client |
| **WRONGTYPE error** | Return the proper `WRONGTYPE` error when operating on wrong types |
| **CONFIG command** | Runtime configuration via `CONFIG GET/SET` |

---

## Project Structure

```
redis-clone/
├── cmd/
│   └── main.go              # TCP server, connection loop, AOF replay
├── internal/
│   ├── util.go              # RESP reading helpers (ReadLine, ReadBulkString)
│   ├── aof/
│   │   └── aof.go           # AOF open, append, replay, fsync modes
│   ├── command/
│   │   ├── type.go          # CommandContext struct
│   │   ├── resp_writer.go   # RESP response encoding
│   │   ├── basic.go         # PING, ECHO, QUIT
│   │   ├── string.go        # SET, GET, DEL
│   │   ├── list.go          # LPUSH, RPUSH, LRANGE
│   │   ├── set.go           # SADD, SMEMBERS
│   │   ├── hash.go          # HSET, HGET
│   │   └── ttl.go           # EXPIRE, TTL
│   ├── dispatcher/
│   │   └── dispatcher.go    # Command routing switch
│   └── store/
│       └── store.go         # In-memory data store (all types + TTL checker)
├── Dockerfile
├── docker-compose.yml
├── appendonly.aof
└── go.mod
```

---

## Legend

| Symbol | Meaning |
|---|---|
| ✅ | Fully implemented |
| ⚠️ | Partially implemented |
| ❌ | Not yet implemented |
