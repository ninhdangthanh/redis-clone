# Redis Clone – Full Implementation Plan

This document outlines a complete, ordered, and practical roadmap for building a Redis clone in Go.

---

## 1. Connection Layer
- Implement a TCP server.
- Accept multiple client connections.
- Handle basic connection lifecycle:
  - read → process → write → close

---

## 2. Basic Commands
- Implement simple command parsing (space-based since RESP is skipped).
- Create a command router/dispatcher.
- Support basic commands:
  - `PING`
  - `ECHO`
  - `SET`
  - `GET`

---

## 3. Authentication
- Add server config for password.
- Track connection authentication state.
- Implement:
  - `AUTH`
  - `QUIT`

---

## 4. Core Key-Value Store
- Use an in-memory map for storing keys:
  - `map[string]Value`
- Introduce value types: string, list, set, hash.
- Implement type checking and error messages:
  - Wrong type operations.
  - Missing key handling.

---

## 5. TTL (Time To Live)
- Support expiration commands:
  - `EXPIRE`
  - `TTL`
- **Passive expiration**: check key expiry on access.
- **Active expiration**: background goroutine scanning expired keys.

---

## 6. Data Structures
### List
- `LPUSH`
- `RPUSH`
- `LPOP`
- `RPOP`
- `LRANGE`

### Set
- `SADD`
- `SREM`
- `SMEMBERS`

### Hash
- `HSET`
- `HGET`
- `HGETALL`
- `HDEL`

---

## 7. Persistence

### Append-Only File (AOF)
- Append each command to AOF.
- Replay AOF at startup to restore state.
- Support fsync modes (always / every second / never).

### AOF Rewrite (Compaction)
- Create minimal version of the database in a new AOF.
- Swap old AOF file atomically.
- Run in the background.

### RDB Snapshotting (Optional, Later)
- Take periodic snapshots for faster startup.

---

## 8. Eviction & Memory Management
- Add configurable `maxmemory`.
- Implement approximate eviction algorithms:
  - LRU
  - LFU
  - FIFO
- Background eviction loop to free memory.

---

## 9. Pub/Sub
- Maintain subscriber lists for channels.
- Implement:
  - `SUBSCRIBE`
  - `UNSUBSCRIBE`
  - `PUBLISH`
- Broadcast messages to subscribers.
- Handle blocking connections.

---

## 10. Transactions (MULTI/EXEC)
- Implement transaction queue per connection.
- Support:
  - `MULTI`
  - `EXEC`
  - `DISCARD`
- Ensure atomic execution of queued commands.
- Handle errors inside queued commands.

---

## 11. Graceful Shutdown
- Catch termination signals (SIGTERM, SIGINT).
- Flush AOF before exit.
- Stop background workers:
  - TTL expiration goroutine
  - AOF rewrite
  - Eviction loop
- Close all client connections cleanly.

---

## Optional Advanced Features (Future)
- Replication (MASTER/REPLICA)
- WATCH / UNWATCH (optimistic locking)
- Cluster mode (hash slots)
- Slowlog
- INFO command
- MONITOR command

---