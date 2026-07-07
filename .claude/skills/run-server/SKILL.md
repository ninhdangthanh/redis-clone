---
name: run-server
description: Use when running, starting, hot-reloading, or manually smoke-testing this redis-clone server, or when confirming a change works against a live server (e.g. "run the server", "start it and test SET/GET", "does eviction actually kick in?", "reproduce this against a running instance"). Covers ports, .env config, redis-cli usage, and the RESP-only protocol.
---

# Running & smoke-testing redis-clone

> For the layer map / concurrency model, see the **architecture-overview** skill.

## Protocol: RESP on both sides — use `redis-cli`

The request side is **RESP arrays**, not plain text. `readRESPCommand` in
`cmd/main.go` rejects any line not starting with `*` (`ERR only RESP arrays
supported`). So the practical client is a real Redis CLI:

```bash
redis-cli -p 6379 PING            # one-shot
redis-cli -p 6379                 # interactive session
redis-cli -p 6379 SET k v EX 60
redis-cli -p 6379 GET k
```
`nc` / `telnet` only work if you hand-type RESP framing — avoid them. The README
calling the protocol "space-based" is **outdated**; trust the code.

## Starting the server

The port is hard-coded to **`:6379`** (`net.Listen("tcp", ":6379")` in `main`).

| Command | What it does |
|---|---|
| `make run` | `go run ./cmd` — foreground, uses `.gocache` |
| `make dev` | Air hot-reload (installs `bin/air` first run, config in `.air.toml`) |
| `make build` | binary → `bin/redis-server` |
| `make test` | `go test ./...` |

On startup it **replays `appendonly.aof`** (restoring prior state) before
listening — see the `debug-aof` skill if replay fails or old keys reappear.

Run in the background so you can drive it, then always stop it:
```bash
make run   # (run as a background process)
redis-cli -p 6379 PING   # expect PONG
# ... exercise the change ...
# stop it: Ctrl-C, or kill the process. Shutdown flushes AOF + drains conns.
```
Shutdown is graceful (SIGINT/SIGTERM → drain clients, stop workers, close AOF),
so let it exit cleanly rather than `kill -9` when you care about AOF durability.

## Config via .env (and env overrides)

`loadEnvFile(".env")` loads `.env`, but **real environment variables win**
(`loadEnvFile` skips keys already set). So override per-run on the command line:

```bash
# Force eviction to trigger: tiny limit + a policy
MAXMEMORY=1897 MAXMEMORY_POLICY=allkeys-lru go run ./cmd
```

Keys read (`storeConfigFromEnv`):
- `MAXMEMORY` — bytes; `0`/empty/invalid → limit disabled.
- `MAXMEMORY_POLICY` — `noeviction` (default), `allkeys-lru`, `volatile-lru`,
  `allkeys-lfu`, `allkeys-random`. Unknown values fall back to `noeviction`
  (`ParseEvictionPolicy`).

The store prints `[STORE] SET …` and `[STORE] EVICT …` lines to stdout — watch
them to confirm memory accounting and eviction actually fire.

## Confirming a change end-to-end

Prefer this over trusting unit tests alone for anything touching the wire:
1. Start the server (background), tail its stdout.
2. Drive the exact commands your change affects with `redis-cli`.
3. Assert the reply **and** any `[STORE]` log lines.
4. For persistence changes, restart and verify state survived (see `debug-aof`).
5. Stop the server.

## Docker (optional)

`make compose-up` / `compose-down` / `compose-logs` run it via
`docker-compose.yml`; `make docker-build` builds the image. Same `:6379`.

## Gotchas
- Port 6379 already in use (a real Redis, or a leftover instance) → `Failed to
  start server`. Kill the other listener or free the port.
- Stale `appendonly.aof` in the repo root makes "old" keys reappear on start —
  that's replay working, not a bug. Truncate the file to start clean.
- `.env` changes don't apply if the same var is exported in your shell — the
  shell value wins.
