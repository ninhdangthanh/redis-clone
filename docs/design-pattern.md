# Redis Clone – Design Patterns in the Current Codebase

Tài liệu này mô tả **pattern đang tồn tại trong code hiện tại**, không phải danh sách pattern dự định refactor trong tương lai. Mục tiêu là giữ kiến trúc đơn giản, dễ đọc và phù hợp với Redis clone nhỏ bằng Go.

## Tổng quan

| Pattern / kỹ thuật | Trạng thái | Vị trí chính |
| --- | --- | --- |
| Observer / Mediator | Đang dùng | `internal/pubsub/hub.go` |
| Simple Factory | Đang dùng | `NewStore*`, `NewHub*`, `aof.Open`, `NewServer` |
| Dependency Injection | Đang dùng | `cmd/server.go`, `CommandContext` |
| Facade / lifecycle coordinator | Đang dùng | `cmd/server.go` |
| Strategy-like policy selection | Đang dùng, nhưng không phải GoF Strategy đầy đủ | `internal/store/store.go`, `internal/aof/aof.go` |
| State machine tường minh | Đang dùng ở mức nhỏ | `cmd/main.go` |
| Command, Decorator, Chain of Responsibility, Builder, Singleton | Không dùng | — |

## 1. Observer và Mediator – Pub/Sub ✅

`Hub` là trung tâm điều phối giữa publisher và subscriber. Publisher không biết subscriber cụ thể nào; subscriber cũng không gọi trực tiếp publisher. Đây vừa là một biến thể thực tế của Observer, vừa có đặc điểm của Mediator.

```text
PUBLISH channel payload
          │
          ▼
        Hub.Publish
          │
          ▼
Subscriber.Messages (buffered channel)
```

Các thành phần chính:

- `Hub.channels` lưu danh sách subscriber theo channel.
- `NewSubscriber` tạo subscriber với buffered `Messages` channel và ID duy nhất.
- `Subscribe`, `Unsubscribe`, `UnsubscribeAll` quản lý quan hệ đăng ký.
- `Publish` snapshot subscribers dưới read lock rồi fan-out message ra từng subscriber.

File: [`internal/pubsub/hub.go`](../internal/pubsub/hub.go)

> Không có `Subscriber` interface hay phương thức `Notify`. Go channel là cơ chế notification thực tế của project.

## 2. Simple Factory – các hàm khởi tạo ✅

Project dùng hàm khởi tạo thay cho Factory Method/Abstract Factory class-based. Đây là idiom tự nhiên hơn trong Go.

| Constructor | Trách nhiệm |
| --- | --- |
| `store.NewStore()` / `NewStoreWithConfig` | Tạo in-memory store, chuẩn hoá eviction policy mặc định. |
| `pubsub.NewHub()` / `NewHubWithBuffer` | Tạo hub và cấu hình buffer subscriber. |
| `aof.Open(path, mode)` | Mở AOF, tạo writer và worker fsync khi cần. |
| `NewServer(listener, store, aof, hub)` | Ghép các dependency của server lifecycle. |
| `command.NewRespWriter(writer)` | Bọc `bufio.Writer` thành RESP writer. |

Các file: `internal/store/store.go`, `internal/pubsub/hub.go`, `internal/aof/aof.go`, `cmd/server.go`, `internal/command/resp_writer.go`.

> Đây không phải Abstract Factory: project chỉ có một backend store in-memory và không có họ object thay thế lẫn nhau.

## 3. Dependency Injection – server và command context ✅

`Server` không tự tạo store, AOF hay Pub/Sub hub. Các dependency được truyền vào `NewServer`:

```go
server := NewServer(listener, store, aofFile, pubsubHub)
```

Tương tự, command handler nhận `*command.CommandContext`, chứa `Store`, `PubSub`, `Writer` và trạng thái auth. Cách này làm handler độc lập với TCP listener và dễ test hơn.

Các file: [`cmd/server.go`](../cmd/server.go), [`internal/command/type.go`](../internal/command/type.go).

## 4. Facade / Lifecycle Coordinator – graceful shutdown ✅

`Server` tập trung vòng đời của các subsystem mà `main` không nên điều phối thủ công:

1. Nhận connection từ listener.
2. Theo dõi connection đang hoạt động.
3. Khởi động TTL và eviction workers.
4. Khi shutdown: đóng listener để nhả port, đóng client sockets, chờ connection/worker kết thúc, sau đó flush + sync + close AOF.

```text
signal → Server.Shutdown
       → listener.Close
       → close active connections
       → wait handlers and workers
       → AOF.Flush + Sync + Close
```

Đây có tính chất Facade: `main` gọi một API `Shutdown(ctx)` thay vì biết chi tiết của từng resource. Nó không cần nằm trong `internal/server`; với scope hiện tại, `cmd/server.go` là vị trí phù hợp.

File: [`cmd/server.go`](../cmd/server.go)

## 5. Strategy-like policy selection – eviction và AOF fsync ✅

### Eviction

`store.Config.EvictionPolicy` chọn thuật toán eviction khi vượt `MaxMemory`:

- `noeviction`
- `allkeys-lru`
- `volatile-lru`
- `allkeys-lfu`
- `allkeys-random`

Selection hiện dùng `EvictionPolicy` enum-like string và `switch` trong store. Các thuật toán là implementation details của `Store`, chưa được tách thành interface `EvictionPolicyStrategy`.

### AOF fsync

`aof.FsyncMode` chọn hành vi persistence:

- `FsyncAlways`
- `FsyncEverySec`
- `FsyncNever`

Tương tự eviction, đây là runtime policy selection bằng enum + `switch`, không phải GoF Strategy đầy đủ với các object strategy.

Các file: [`internal/store/store.go`](../internal/store/store.go), [`internal/aof/aof.go`](../internal/aof/aof.go).

## 6. State machine tường minh – connection mode ✅

Connection có hai mode xử lý thực tế:

- **Normal mode:** command đi qua `dispatcher.Dispatch`.
- **Subscribed mode:** sau `SUBSCRIBE`, vòng lặp chỉ chấp nhận `SUBSCRIBE`, `UNSUBSCRIBE`, `PING`, `QUIT` và đồng thời đọc `Subscriber.Messages`.

Ngoài ra `CommandContext.Authenticated` là trạng thái auth hiện tại (tạm thời luôn `true` cho đến khi `AUTH` được hoàn tất).

State được biểu diễn bằng nhánh điều kiện và hàm `serveSubscribedConn`, chưa cần tách thành `ClientState` interface. Với hai mode hiện tại, cách này ít boilerplate và rõ luồng hơn State pattern cổ điển.

File: [`cmd/main.go`](../cmd/main.go)

## 7. Command routing – dispatch table bằng switch ✅

`dispatcher.Dispatch` chuẩn hoá command name và route sang handler nhóm:

```go
case "SET", "GET", "DEL":
    return command.HandleStringCommands(ctx, args)
```

Đây là command routing, **không phải GoF Command pattern**: không có `Command` interface, command object hay queue `[]Command`. Cấu trúc hiện tại phù hợp khi danh sách command còn nhỏ và không có `MULTI`/`EXEC`.

File: [`internal/dispatcher/dispatcher.go`](../internal/dispatcher/dispatcher.go)
