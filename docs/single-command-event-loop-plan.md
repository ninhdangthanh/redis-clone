# Single Command Event Loop Plan

## Goal

Make normal Redis commands execute in one server-owned loop. Many clients may
read and write TCP sockets concurrently, but only the event loop runs normal
client command handlers. TTL and eviction remain background workers, protected
by the Store mutex.

## Design

```text
clients (many TCP connections)
        |
        v
per-connection reader goroutines
        |
        v
bounded command queue (256 requests)
        |
        v
one CommandExecutor goroutine
  - dispatcher
  - Store access
  - AOF append
  - response write / flush
```

Each connection submits one command and waits for its completion before reading
the next command. That preserves response order for pipelined requests while
the bounded queue provides backpressure under load.

## Implementation phases

- [x] Add `CommandExecutor`, with one request queue and one execution goroutine.
- [x] Route normal commands such as `GET`, `SET`, collections, TTL, `PUBLISH`,
  `INFO`, and `QUIT` through that executor.
- [x] Start the executor with the server context and wait for it during shutdown.
- [x] Keep socket reading concurrent; it is I/O waiting, not database execution.
- [x] Keep subscribed connections on their dedicated message-writing path.
  `PUBLISH` from normal clients still passes through the event loop; subscription
  bookkeeping remains protected by the Pub/Sub hub mutex.
- [x] Add coverage with 20 simultaneous clients submitting commands.
- [ ] Decide whether Store's mutex should remain. It is still useful for TTL and
  eviction workers; removing it safely needs those workers to submit work to the
  event loop as well.

## Non-goals

This does not attempt to copy Redis's exact networking implementation. Go still
uses goroutines for accepting clients and waiting on socket reads. The important
single-threaded property is that command handlers access the database in one
deterministic sequence.
