---
name: debug-aof
description: Use when working on, debugging, or reasoning about AOF persistence in this redis-clone — a command not surviving restart, keys wrongly reappearing, replay failing, fsync/durability questions, or extending what gets persisted (e.g. "my new command vanishes after restart", "why does this key come back?", "AOF replay error", "add fsync always mode"). Covers the persist gate, arg rewriting, replay, and fsync modes.
---

# Debugging AOF persistence

> For where AOF sits in the whole system, see the **architecture-overview** skill.

The AOF (`appendonly.aof` in the repo root) is an append-only log of **write
commands** in RESP-array format, replayed on startup to rebuild state. Code lives
in `internal/aof/aof.go`; wiring is in `cmd/main.go`.

## The write path (append)

In `handleConn` (`cmd/main.go`), after a command runs:
```go
if commandContext.Success && aofFile.ShouldPersistCommand(cmd) {
    aofFile.Append(aof.ArgsForAppend(args, time.Now()))
}
```
Two gates must **both** pass, or nothing is written:
1. **`commandContext.Success == true`** — the handler returned `true`. A handler
   that returns `false` on an error path is never persisted (correct). But a
   handler that wrongly returns `true` on failure **will** persist a bad command.
2. **`ShouldPersistCommand(cmd)`** — the uppercase verb is in the `writeCommands`
   map. **This is the #1 cause of "my command vanishes after restart":** the
   handler works live but the verb was never added to that map. Fix: add it (see
   the `add-redis-command` skill, layer 4).

Read-only verbs (GET, LRANGE, SMEMBERS, TTL, INFO, PING) must **not** be in the
map — persisting them would bloat the log and, on replay, they'd be dispatched
pointlessly.

## Arg rewriting before append — `ArgsForAppend`

Commands are **normalized to absolute time** before being written, so replay is
deterministic regardless of when it happens:
- `SET k v EX 60` → `SET k v PXAT <now+60000ms>`
- `SET k v PX 500` → `SET k v PXAT <now+500ms>`
- `EXPIRE k 60` → `PEXPIREAT k <now+60000ms>`

That's why `PEXPIREAT` and `PXAT` exist in the store/dispatcher even though a
client rarely sends them — they're the **replay-safe forms**. If you add a new
command with relative-time semantics, rewrite it here too, and make sure its
absolute-time form is dispatchable and in `ShouldPersistCommand`.

If you see a *relative* TTL command in the AOF file, `ArgsForAppend` isn't being
called for it — check the append site.

## The replay path (startup)

`ReplayAOF` (`cmd/main.go`) → `aofFile.Replay(cb)`:
- Seeks to file start, parses each RESP array, calls the callback per command.
- The callback **re-checks `ShouldPersistCommand`** (skips anything else) and
  dispatches into the store with a **silent writer** (`io.Discard`) and
  `Authenticated: true`.
- If any dispatch returns `false`, replay aborts with
  `failed to replay command: <args>`.

Implications when debugging:
- **Keys reappear after you "deleted" them** → they're in `appendonly.aof` and
  being replayed. Expected. To start clean, truncate the file:
  `: > appendonly.aof` (or `rm` it — it's recreated with `O_CREATE`).
- **`invalid aof format` / `invalid bulk header`** → the file is corrupted or
  hand-edited with wrong framing. RESP is exact: `*N\r\n` then N × `$len\r\n<bytes>\r\n`.
- **`failed to replay command`** → a persisted command now fails to dispatch
  (e.g. you changed arg validation, or removed a verb the log still contains).
  Either restore compatibility or clear the AOF.
- Replay logs `Replayed command: <VERB>` per entry to stdout — use it to see
  exactly what's being restored.

## fsync / durability modes

`FsyncMode` (set in `main` via `aof.Open("appendonly.aof", aof.FsyncEverySec)`):

| Mode | Behavior | Durability |
|---|---|---|
| `FsyncAlways` | flush + `f.Sync()` on every `Append` | strongest, slowest |
| `FsyncEverySec` (default) | background goroutine flushes+syncs every 1s | ~1s data loss window |
| `FsyncNever` | flush to OS buffer only, no `Sync` | OS decides; fastest |

`ParseMode` maps `"always"/"never"/else→everysec`. Note `Open` is currently
called with a hard-coded `FsyncEverySec` — the mode isn't wired to config/env
yet, so changing it means editing `main` (or adding an env knob + `ParseMode`).

`Close()` (called during graceful shutdown) does a final flush + sync + close via
`closeOnce`, so a clean shutdown loses nothing even in `EverySec` mode. A hard
kill (`kill -9`) can lose up to the last second.

## Inspecting the file

It's RESP text — readable directly:
```bash
cat -A appendonly.aof     # shows \r\n framing explicitly
wc -l appendonly.aof
```
Each command is one `*N` array. If a write you expected is missing, the command
either failed (`Success=false`) or isn't in `ShouldPersistCommand`.

## Tests

`internal/aof/aof_test.go` covers append/replay round-trips. When adding a
persisted command, add a test that appends it, reopens/replays, and asserts the
store state was restored.
