# Redis Clone – GoF Design Patterns Mapping

## 1. Strategy Pattern ✅

### Mục đích

Thay đổi thuật toán tại runtime mà không sửa code gọi.

### Áp dụng

#### Phase 7 – AOF Fsync

Hiện tại:

```go
type FsyncMode int

const (
    FsyncAlways
    FsyncEverySec
    FsyncNever
)
```

Có thể refactor thành:

```go
type SyncStrategy interface {
    Sync(file *os.File) error
}
```

Implementations:

```go
AlwaysSyncStrategy
EverySecondSyncStrategy
NeverSyncStrategy
```

### File

```text
internal/aof/
├── strategy.go
├── sync_always.go
├── sync_everysec.go
└── sync_never.go
```

---

#### Phase 8 – Eviction

Redis hỗ trợ:

* allkeys-lru
* volatile-lru
* allkeys-random
* allkeys-lfu
* noeviction

Đây là ví dụ kinh điển của Strategy.

```go
type EvictionPolicy interface {
    Evict(store *Store)
}
```

### File

```text
internal/eviction/
├── policy.go
├── lru.go
├── lfu.go
├── random.go
└── noeviction.go
```

---

## 2. Command Pattern ✅

### Mục đích

Biến command thành object.

### Áp dụng

Dispatcher hiện tại:

```go
switch cmd {
case "GET":
case "SET":
}
```

Refactor:

```go
type Command interface {
    Execute(ctx *CommandContext)
}
```

Commands:

```go
SetCommand
GetCommand
DelCommand
```

Registry:

```go
map[string]Command
```

### File

```text
internal/command/
├── command.go
├── set_command.go
├── get_command.go
└── ...
```

### Bonus

Phase 10 MULTI/EXEC gần như cần Command Pattern.

Queue:

```go
[]Command
```

EXEC:

```go
for _, cmd := range queue {
    cmd.Execute(...)
}
```

---

## 3. Observer Pattern ✅

### Áp dụng

Phase 9 Pub/Sub

```go
type Subscriber interface {
    Notify(msg Message)
}
```

### File

```text
internal/pubsub/
├── subscriber.go
├── publisher.go
└── pubsub.go
```

---

## 4. Factory Method ✅

### Áp dụng

Tạo command từ RESP.

Thay vì:

```go
switch args[0]
```

```go
func NewCommand(args []string) Command
```

### File

```text
internal/command/factory.go
```

---

## 5. Abstract Factory ⚠️

### Áp dụng

Tạo backend storage.

Hiện tại:

```go
Store
```

Sau này:

```go
MemoryStore
RDBStore
ClusterStore
```

Factory:

```go
type StoreFactory interface {
    CreateStore() Store
}
```

### File

```text
internal/store/factory.go
```

---

## 6. Singleton ⚠️

### Áp dụng

PubSub Manager

```go
var (
    pubsub *PubSub
    once sync.Once
)
```

### File

```go
internal/pubsub/manager.go
```

Go thường dùng dependency injection hơn Singleton.

---

## 7. Builder Pattern ⚠️

### Áp dụng

Config server.

```go
server := NewServerBuilder().
    WithPort(6379).
    WithAOF().
    WithTTL().
    Build()
```

### File

```text
internal/server/
├── builder.go
└── server.go
```

---

## 8. Decorator Pattern ✅

### Áp dụng

Command middleware.

Ví dụ:

```text
AUTH
LOGGING
METRICS
RATE LIMIT
```

Decorator:

```go
type Handler interface {
    Execute(...)
}
```

Wrapper:

```go
AuthDecorator
LoggingDecorator
MetricsDecorator
```

### File

```text
internal/middleware/
├── auth.go
├── logging.go
└── metrics.go
```

---

## 9. Chain of Responsibility ✅

### Áp dụng

Authentication pipeline.

```go
Client
  ↓
AuthCheck
  ↓
RateLimit
  ↓
Dispatcher
```

### File

```text
internal/pipeline/
├── handler.go
├── auth.go
├── ratelimit.go
└── dispatch.go
```

---

## 10. State Pattern ✅

### Áp dụng

Connection state.

Hiện tại:

```go
Authenticated bool
```

Sau này:

```go
Unauthenticated
Authenticated
Subscribed
Transaction
Closing
```

```go
type ClientState interface {
    Handle(...)
}
```

### File

```text
internal/client/state.go
```

---

## 11. Template Method ⚠️

### Áp dụng

Command execution flow.

```go
Validate()
Authenticate()
Execute()
Persist()
Respond()
```

### File

```text
internal/command/base.go
```

---

## 12. Proxy Pattern ⚠️

### Áp dụng

Replication.

```text
Client
  ↓
Replica Proxy
  ↓
Master
```

### File

```text
internal/replication/proxy.go
```

---

## 13. Adapter Pattern ⚠️

### Áp dụng

Thay đổi storage backend.

```go
RedisStoreAdapter
MemoryStoreAdapter
```

### File

```text
internal/store/adapter.go
```

---

## 14. Facade Pattern ✅

### Áp dụng

Server bootstrap.

Hiện tại main.go đang làm quá nhiều.

Tạo:

```go
server := NewServer(...)
server.Start()
```

### File

```text
internal/server/server.go
```

---

## 15. Mediator Pattern ⚠️

### Áp dụng

PubSub Manager.

```text
Publisher
     ↓
  PubSub
     ↓
Subscriber
```

Publisher và subscriber không biết nhau.

### File

```text
internal/pubsub/pubsub.go
```

---

# Patterns không nên dùng

## Flyweight ❌

Không đáng cho project này.

## Visitor ❌

Redis command model không phù hợp.

## Interpreter ❌

Chỉ hữu ích nếu làm Lua scripting.

## Bridge ❌

Overengineering.

## Composite ❌

Redis không có cấu trúc cây.

## Prototype ❌

Ít giá trị.

## Memento ❌

Snapshot/RDB không cần pattern này.

---

# Các pattern mình khuyên thực sự nên làm

## Phase 2

* Command
* Factory

## Phase 3

* State
* Chain of Responsibility

## Phase 7

* Strategy
* Decorator

## Phase 8

* Strategy

## Phase 9

* Observer
* Mediator

## Phase 10

* Command
* State

## Toàn hệ thống

* Facade
* Builder

Đây là khoảng **10–12 GoF patterns** có ứng dụng tự nhiên vào Redis clone mà không làm code trở nên gượng ép hay "pattern vì pattern".
