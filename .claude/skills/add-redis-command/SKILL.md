---
name: add-redis-command
description: Use when adding, extending, or wiring up a Redis command in this Go redis-clone (e.g. "add the SREM command", "implement HDEL", "support GETSET", "add RPOP"). Walks the full store → command → dispatcher → AOF → test flow so no layer is missed.
---

# Adding a Redis command to redis-clone

> New to the codebase? Read the **architecture-overview** skill first for the
> layer map and concurrency invariants this skill assumes.

A command in this codebase is not a single function — it threads through up to
six layers. Skipping one produces silent bugs: a write that doesn't survive
restart (missing AOF), an unauthenticated write (missing auth check), or a
command the router never reaches (missing dispatcher case).

Do the layers **in this order** and stop to build/test at the end.

## Layer checklist

1. **Store** (`internal/store/store.go`) — the actual data operation.
2. **Command handler** (`internal/command/<group>.go`) — arg parsing + RESP output.
3. **Dispatcher** (`internal/dispatcher/dispatcher.go`) — route the verb to the handler.
4. **AOF persistence** (`internal/aof/aof.go`) — only if the command **writes** (mutates state).
5. **Tests** (`internal/command/command_test.go`, `internal/store/store_test.go`).
6. **README.md** — add the verb under its data-structure section.

Read one sibling command end-to-end before writing (e.g. `SADD`/`SMEMBERS` for a
set op, `LPUSH`/`LRANGE` for a list op) and mirror its shape.

---

## 1. Store layer

Every method locks `s.mu`, resolves/checks the value, and returns raw results.
Follow these rules — they are load-bearing:

- **Writes go through `writeLocked`** so eviction + rollback + memory limits apply:
  ```go
  func (s *Store) SRem(key string, member []byte) (bool, error) {
      s.mu.Lock()
      defer s.mu.Unlock()

      removed := false
      err := s.writeLocked(key, func(now int64) error {
          v, ok := s.data[key]
          if ok && s.valueExpiredLocked(key, v, now) {
              ok = false
          }
          if !ok {
              return nil // missing key: nothing to remove
          }
          if v.Type != SetType {
              return ErrWrongType
          }
          if _, exists := v.Set[string(member)]; exists {
              delete(v.Set, string(member))
              removed = true
          }
          s.touchValueLocked(v, now)
          return nil
      })
      return removed, err
  }
  ```
- **Type check** any existing value and return `ErrWrongType` on mismatch.
- **Expiry check** with `s.valueExpiredLocked(key, v, now)` before using a value
  (it lazily deletes expired keys). Reads use `time.Now().UnixMilli()` for `now`.
- **Call `s.touchValueLocked(v, now)`** on access — it maintains
  `LastAccessAt`/`AccessCount` that LRU/LFU eviction depends on.
- **Defensive-copy every `[]byte`** in and out: `append([]byte(nil), b...)`.
  Never hand a caller a slice that aliases stored data, and never store a slice
  the caller still holds.
- Read-only methods (like `SMembers`) lock `s.mu` directly (no `writeLocked`),
  still expiry-check + `touchValueLocked`, and return copies.

## 2. Command handler

Handlers live in the file for their data type: `basic.go`, `string.go`,
`list.go`, `set.go`, `hash.go`, `ttl.go`, `pubsub.go`, `info.go`. Signature is
always `func(ctx *CommandContext, args []string) bool` — `args[0]` is the verb.

There is a **group handler** per file (e.g. `HandleSetCommands`) that does the
auth check once and switches to per-verb handlers. Add write verbs there:

```go
func handleSRem(ctx *CommandContext, args []string) bool {
    if len(args) != 3 { // SREM key member
        ctx.Writer.WriteError("wrong number of arguments for SREM")
        return false
    }
    removed, err := ctx.Store.SRem(args[1], []byte(args[2]))
    if err != nil {
        writeStoreError(ctx, err) // maps ErrWrongType / ErrMaxMemory to Redis errors
        return false
    }
    if removed {
        ctx.Writer.WriteInteger(1)
    } else {
        ctx.Writer.WriteInteger(0)
    }
    return true
}
```

Rules:
- **Auth**: write commands must reject when `!ctx.Authenticated` with
  `-NOAUTH Authentication required`. The group handlers already do this at the
  top — keep new verbs inside a guarded group handler rather than routing them
  raw from the dispatcher.
- **Validate `len(args)` first** and emit `wrong number of arguments for <VERB>`.
- **Return `true` on success, `false` on any error** (the return controls AOF
  persistence in `main.go`; a `false` command is never appended).
- **Use `writeStoreError(ctx, err)`** for store errors — don't hand-write
  WRONGTYPE/OOM strings.

### RESP writer reference (`internal/command/resp_writer.go`)

| Method | Wire output | Use for |
|---|---|---|
| `WriteSimpleString(s)` | `+s\r\n` | `OK`, `PONG` |
| `WriteError(msg)` | `-msg\r\n` | errors |
| `WriteBulkString(b)` | `$len\r\n b \r\n` | a string value |
| `WriteNull()` | `$-1\r\n` | missing key on GET-like reads |
| `WriteInteger(n)` | `:n\r\n` | counts / booleans (0/1) |
| `WriteArrayHeader(n)` + N writes | `*n\r\n …` | multi-value replies |

Array reply pattern (see `handleLRange`):
```go
ctx.Writer.WriteArrayHeader(len(values))
for _, v := range values {
    ctx.Writer.WriteBulkString(v)
}
```

## 3. Dispatcher

Add the verb to the switch in `internal/dispatcher/dispatcher.go`, grouped with
its siblings so it reaches the auth-guarded group handler:

```go
case "SADD", "SMEMBERS", "SREM":
    return command.HandleSetCommands(ctx, args)
```
Then add the `case "SREM":` inside that group handler's own switch (layer 2).

## 4. AOF persistence — writes only

If the command mutates state, add the **uppercase verb** to the map in
`ShouldPersistCommand` (`internal/aof/aof.go`), or it won't be replayed on
restart:
```go
writeCommands := map[string]bool{
    "SET": true, "DEL": true, "LPUSH": true, "RPUSH": true,
    "SADD": true, "SREM": true, // ← new
    "HSET": true, "EXPIRE": true, "PEXPIREAT": true,
}
```
Read-only commands (GET, LRANGE, SMEMBERS, TTL, INFO, PING…) must **not** be added.
If the command needs custom rewriting before persistence (like SET's TTL
normalization), extend `ArgsForAppend`; otherwise the raw args are appended.

## 5. Tests

Command tests build a context with `newCommandTestContext()` and assert the
**raw RESP bytes** written to the buffer:
```go
ctx, buf := newCommandTestContext() // Authenticated: true, fresh store
ctx.Store.SAdd("s", []byte("a"))
if !HandleSetCommands(ctx, []string{"SREM", "s", "a"}) {
    t.Fatal("SREM returned false")
}
if got := buf.String(); got != ":1\r\n" {
    t.Fatalf("SREM response = %q, want :1", got)
}
```
Also cover: wrong-arg-count, WRONGTYPE against a key of another type, and (for
writes) the unauthenticated path. Add a store-level test in
`internal/store/store_test.go` for the new `Store` method.

## 6. README

Add the verb under its data-structure heading in `README.md` (Section 6 for
List/Set/Hash, etc.) to keep the command list current.

---

## Finish: build + test

```bash
make test          # go test ./...
make build         # go build ./cmd
```
Both must pass. To smoke-test by hand: `make run`, then in another terminal use
`redis-cli -p 6379` and type your command. The server speaks **RESP arrays on
both request and response** (`readRESPCommand` rejects anything not starting
with `*`), so a real `redis-cli` is the simplest client — `nc` would require you
to hand-type RESP. (The README's "space-based" note is outdated.)

## Common misses
- Forgot `ShouldPersistCommand` → command works live, vanishes after restart.
- Returned `true` on an error path → a failed command gets persisted to AOF.
- Skipped `touchValueLocked` → key becomes wrongly favored/starved by eviction.
- Returned a stored `[]byte` without copying → later mutation corrupts the store.
- Added the dispatcher case but routed it around the group handler → no auth check.
