# Phase 10: Graceful Shutdown và `context.Context`

Tài liệu này giải thích cách Redis clone dừng tiến trình một cách có kiểm soát: ngừng nhận client mới, báo các goroutine đang chạy dừng lại, giải phóng socket, rồi bảo đảm AOF đã được flush và sync trước khi process kết thúc.

Phase này cũng là bài tập quan trọng về `context.Context`: context không “kill” goroutine; nó là tín hiệu để code bên trong goroutine chủ động dừng.

## 1. Graceful shutdown là gì?

Khi nhận `Ctrl+C` (`SIGINT`) hoặc `SIGTERM`, server không nên exit ngay lập tức. Nếu exit ngay:

- client đang gửi command có thể bị ngắt giữa chừng;
- goroutine TTL, eviction và AOF fsync có thể còn chạy;
- buffer AOF có thể chưa được ghi xuống disk;
- tài nguyên vẫn được OS thu gom, nhưng không theo thứ tự mà application mong muốn.

Graceful shutdown có một thứ tự có chủ đích:

```text
SIGINT / SIGTERM
  -> main gọi Server.Shutdown
  -> đóng listener: không Accept client mới, trả TCP port
  -> cancel server context: báo worker và handler dừng
  -> đóng client sockets: ngắt network Read đang block
  -> đợi connection handler và worker kết thúc
  -> flush, sync, close AOF
  -> process exit
```

`Server` trong `cmd/server.go` là lifecycle coordinator: `main` chỉ gọi `server.Shutdown(ctx)`, không cần biết chi tiết đóng từng subsystem.

## 2. Các thành phần của Phase 10

| Thành phần | File | Trách nhiệm |
| --- | --- | --- |
| `signal.NotifyContext` | `cmd/main.go` | Chuyển `SIGINT` / `SIGTERM` thành context cancellation. |
| `Server` | `cmd/server.go` | Sở hữu listener, active connections, worker lifecycle và AOF shutdown. |
| `s.ctx`, `s.cancel` | `cmd/server.go` | Tín hiệu chung để dừng TTL, eviction và connection handler. |
| `connWG` | `cmd/server.go` | Đợi toàn bộ connection handler kết thúc. |
| `workers` | `cmd/server.go` | Giữ `done` channel của TTL và eviction workers. |
| `AOF.Close` | `internal/aof/aof.go` | Dừng fsync worker, flush, sync và close file. |
| `StartTTLCheckerContext` / `StartEvictionCheckerContext` | `internal/store/store.go` | Worker có thể dừng khi nhận cancellation. |

## 3. `context.Context` là gì?

`context.Context` mang thông tin về lifetime của một operation. Nó thường truyền:

- tín hiệu hủy qua `Done()`;
- lý do hủy qua `Err()`;
- deadline/timeout qua `Deadline()`;
- request-scoped values qua `Value()` — dùng tiết kiệm và có chủ đích.

Context không đại diện cho một process hay một goroutine. Không có quy tắc “một goroutine chỉ có một context”. Một process có thể có nhiều context, và một goroutine cũng có thể nhận hoặc dùng context của nhiều scope khác nhau.

Context thường tạo thành cây lifetime:

```text
context.Background()
├── signalCtx          (cancel khi nhận SIGINT/SIGTERM hoặc gọi stop())
├── server.ctx         (cancel khi Server.Shutdown gọi s.cancel())
│   └── connection ctx (child của server.ctx, dừng khi connection kết thúc)
└── shutdownCtx        (deadline 10 giây để chờ shutdown)
```

Quy tắc kế thừa:

- Parent bị cancel thì toàn bộ child của nó cũng bị cancel.
- Cancel child không cancel parent và không ảnh hưởng sibling.
- Function thường nhận `ctx` làm tham số đầu tiên.
- Function tạo `WithCancel`/`WithTimeout` nên gọi `cancel`, thường bằng `defer cancel()`, để giải phóng resource bên trong context khi xong việc.

`Server` lưu `s.ctx` trong struct vì nó là owner của server lifecycle. Còn đa số function chỉ nên nhận và truyền tiếp context, không nên tự ý lưu nó.

## 4. `Background`, `TODO`, `Done` và context cancellable

`context.Background()` là root context. Nó không có deadline, không có cancel function, và:

```go
context.Background().Done() == nil
```

Vì vậy đoạn sau block mãi mãi:

```go
ctx := context.Background()
<-ctx.Done()
```

Receive từ nil channel sẽ block vô hạn. Trong `select`, case từ nil channel bị disable:

```go
select {
case <-context.Background().Done(): // không bao giờ được chọn
case <-ticker.C:
    // case này vẫn chạy
}
```

`context.TODO()` có hành vi tương đương `context.Background()`: cũng không có deadline, không cancel được, `Done()` trả `nil`, và sống đến khi process kết thúc. Khác biệt nằm ở **ý nghĩa dành cho người đọc code**:

```go
context.Background() // chủ động chọn root context cho điểm bắt đầu của chương trình
context.TODO()       // chưa biết context đúng nên là gì; cần quay lại quyết định sau
```

Ví dụ, `main` tạo root context hoặc code khởi tạo ứng dụng dùng `Background()` là hợp lý. Một function tạm thời chưa được truyền context đúng từ caller có thể dùng `TODO()`, nhưng đó nên là dấu nhắc để refactor. Không nên dùng `TODO()` cho worker của server chỉ để “cho code chạy”, vì worker vẫn không có đường dừng.

Để có cancellation path, tạo context bằng một trong các hàm sau:

```go
ctx, cancel := context.WithCancel(parent)
ctx, cancel := context.WithTimeout(parent, 10*time.Second)
ctx, cancel := context.WithDeadline(parent, deadline)
ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
```

`signal.NotifyContext` đã tạo context có thể cancel ở bên trong. Không cần bọc thêm `context.WithCancel` chỉ để dùng `<-signalCtx.Done()`.

`Done()` không tự “throw error”. Nó chỉ là channel bị đóng. Code tự chọn cách phản ứng:

```go
case <-ctx.Done():
    return ctx.Err()
```

`ctx.Err()` là `context.Canceled` nếu ai đó gọi `cancel()`, hoặc `context.DeadlineExceeded` nếu hết timeout/deadline.

## 5. Ai gọi `cancel`?

`cancel` không tự chạy chỉ vì code đang đợi `<-ctx.Done()>`. Phải có event hoặc goroutine gọi nó.

Ví dụ đúng:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go func() {
    time.Sleep(time.Second)
    cancel()
}()

<-ctx.Done() // được unblock sau một giây
```

Ví dụ sau bị kẹt vĩnh viễn:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
<-ctx.Done()
```

`defer cancel()` chỉ chạy khi function sắp `return`, nhưng function lại đang đợi `Done()` để mới có thể return.

Trong Redis clone, các cancellation source là:

| Context | Ai cancel / khi nào |
| --- | --- |
| `signalCtx` | OS gửi `SIGINT`/`SIGTERM`; `stop()` cũng cancel và unregister signal notification. |
| `s.ctx` | `s.shutdown()` gọi `s.cancel()`. |
| Context của connection | `defer cancel()` khi `handleConn` kết thúc; nó cũng bị cancel nếu `s.ctx` bị cancel. |
| `shutdownCtx` | Hết 10 giây, hoặc `defer cancel()` khi `main` kết thúc. |

## 6. Entry point shutdown trong `main`

`main` chạy `Server.Serve()` ở goroutine riêng và đợi signal hoặc lỗi từ `Serve`:

```go
serveErr := make(chan error, 1)
go func() { serveErr <- server.Serve() }()

signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

select {
case <-signalCtx.Done():
    fmt.Println("Shutting down Redis clone...")
case err := <-serveErr:
    // báo lỗi accept/listener
}

shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
err := server.Shutdown(shutdownCtx)
```

`signalCtx` và `server.ctx` là hai context riêng. Chúng không phải parent-child, nhưng flow vẫn đúng: nhận signal thì `main` gọi `Shutdown`, và `Shutdown` cancel `s.ctx`.

## 7. TTL và eviction workers dừng như thế nào?

TTL checker nhận context từ `Server.StartWorkers`:

```go
s.store.StartTTLCheckerContext(s.ctx, time.Second)
```

Trong worker:

```go
for {
    select {
    case <-ctx.Done():
        return
    case <-ticker.C:
        // xóa key hết hạn
    }
}
```

Khi `s.cancel()` chạy, `ctx.Done()` đóng, goroutine `return`, và defer đóng `done` channel:

```go
done := make(chan struct{})
go func() {
    defer close(done)
    // ...
}()
return done
```

`done` khác `ctx.Done()`:

- `ctx.Done()` là mệnh lệnh: “hãy bắt đầu dừng”.
- `done` là xác nhận: “tôi đã dừng xong”.

Worker thuộc server lifecycle không nên được gọi với `context.Background()`. Làm vậy sẽ tạo worker chạy đến khi process chết; `Server.Shutdown` không thể yêu cầu hoặc đợi nó kết thúc.

## 8. `readCommands`, `select` và hủy goroutine

Mỗi TCP connection có goroutine đọc RESP command:

```go
func readCommands(ctx context.Context, r *bufio.Reader) <-chan parsedCommand {
    ch := make(chan parsedCommand, 1)
    go func() {
        defer close(ch)
        for {
            parsed := readRESPCommand(r)
            select {
            case ch <- parsed:
            case <-ctx.Done():
                return
            }
            if parsed.err != nil {
                return
            }
        }
    }()
    return ch
}
```

Trong `select`, operation nằm ngay sau `case`:

```go
case ch <- parsed:      // gửi parsed nếu channel gửi được
case value := <-ch:     // nhận value nếu channel có giá trị hoặc đã đóng
case <-ctx.Done():      // nhận tín hiệu cancellation
```

`case ch <- parsed:` thực sự gửi `parsed` vào `ch`; nó không chỉ là điều kiện kiểm tra. Channel có buffer 1 nên có thể giữ một command trong lúc handler đang xử lý command trước.

Trong `handleConn`:

```go
select {
case <-ctx.Done():
    return
case parsed, ok = <-cmdCh:
}
```

- Nếu server/connection bị cancel: handler return.
- Nếu đọc được command: `parsed` và `ok` được gán, sau đó code xử lý command.
- Nếu `cmdCh` đã đóng: `ok == false`, handler kết thúc.

Nếu cả hai case đều ready, Go chọn ngẫu nhiên một case. Vì thế code sau `select` phải an toàn với bất kỳ case nào được chọn.

## 9. Vì sao cancel context chưa đủ để ngắt network read?

`readRESPCommand(r)` có thể đang block trong `conn.Read()` khi client idle. Context không tự động interrupt system call này.

Vì vậy shutdown làm cả hai việc:

```go
s.cancel()           // báo goroutine đang select trên ctx.Done()
s.closeConnections() // conn.Close() để unblock network Read
```

Sau `conn.Close()`, read trả error; `readCommands` gửi `parsed` có error hoặc thoát vì context đã cancel; `handleConn` kết thúc. Đây là lý do cần đóng socket, không chỉ cancel context.

## 10. `Shutdown` chỉ chạy một lần

Nhiều goroutine có thể gọi `Server.Shutdown` cùng lúc. Hàm phải idempotent: không đóng listener, AOF hoặc channel hai lần.

`shutdownMu`, `shutdownStarted`, `shutdownDone`, `shutdownErr` tạo một “single-flight” nhỏ:

```text
caller 1: Shutdown -> đánh dấu started -> chạy s.shutdown -> lưu err, close(done)
caller 2: Shutdown -> thấy started     -> đợi done      -> trả cùng err
caller 3: Shutdown -> thấy started     -> đợi done hoặc ctx timeout
```

Phần caller đến sau:

```go
select {
case <-done:
    return s.shutdownErr
case <-ctx.Done():
    return ctx.Err()
}
```

Không có exception nào bị ném bởi `Done()`. Nhánh `ctx.Done()` tự chọn `return ctx.Err()` để caller biết nó đã hết thời gian chờ. Shutdown do caller thứ nhất bắt đầu vẫn có thể tiếp tục chạy.

Mutex không được giữ trong lúc đợi `<-done>` hoặc đóng socket/file. Nếu giữ nó, goroutine đang shutdown không thể lock lại để lưu `shutdownErr` và đóng `done`.

## 11. Thứ tự đóng resource và lý do

`s.shutdown(ctx)` làm theo thứ tự:

1. `s.draining.Store(true)`: đánh dấu server đang shutdown; connection vừa accept sẽ bị đóng thay vì được track.
2. `s.listener.Close()`: unblock `Accept`, không nhận client mới và trả port.
3. `s.cancel()`: báo workers/handlers dừng.
4. `s.closeConnections()`: unblock client read đang idle.
5. Đợi `connWG`: bảo đảm không còn handler nào gọi `AOF.Append`.
6. Đợi worker `done` channels: TTL/eviction đã dừng.
7. `s.aof.Close()`: dừng AOF fsync worker, flush, sync, close file.

Đợi handler trước khi close AOF rất quan trọng. Nếu đóng AOF sớm, handler đang xử lý có thể append vào file đã đóng và làm mất command hoặc nhận lỗi không cần thiết.

## 12. `WaitGroup` và channel completion

`connWG` đếm connection handler:

```go
s.connWG.Add(1)
go func() {
    defer s.connWG.Done()
    handleConn(...)
}()
```

`sync.WaitGroup` không có API cho `context`, nên helper bọc `Wait()` vào goroutine và đợi cùng context:

```go
func waitGroupContext(ctx context.Context, wg *sync.WaitGroup) error {
    done := make(chan struct{})
    go func() {
        wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

Tương tự, `waitChannels` đợi từng worker `done` channel. Channel đóng là broadcast: mọi goroutine đang receive từ nó đều được unblock.

## 13. AOF có lifecycle riêng

Nếu AOF dùng mode `FsyncEverySec`, `aof.Open` tạo goroutine `syncEverySecond`. Worker này dừng qua `quitCh`, không dùng server context trực tiếp:

```go
close(a.quitCh)
a.workerWG.Wait()
```

Sau khi fsync worker dừng, `AOF.Close` lock AOF, `Flush`, `Sync`, rồi `Close` file. `sync.Once` đảm bảo AOF chỉ close một lần, tương tự ý tưởng idempotent của `Server.Shutdown`.

## 14. Context trong Gin: request lifetime khác server lifetime

Trong Gin có hai thứ dễ bị gọi chung là “context”:

| Loại | Cách lấy | Lifetime | Dùng cho |
| --- | --- | --- | --- |
| `*gin.Context` | Tham số `c` của handler | Chỉ an toàn trong handler hiện tại. Gin tái sử dụng object này qua `sync.Pool`. | Đọc request, bind input, ghi HTTP response, `c.Get`/`c.Set`. |
| Standard Go context | `c.Request.Context()` | Chỉ sống trong HTTP request. | DB query, gRPC/HTTP call phục vụ response hiện tại. |
| Application context | Tạo khi khởi động server, ví dụ `appCtx` hoặc `s.ctx` | Đến khi server shutdown. | Worker, interval, queue consumer, async task thuộc server. |

Request context bị cancel khi client hủy/đóng request, request hết deadline, hoặc handler đã trả về. Vì thế truyền nó cho downstream call là đúng khi downstream call thuộc chính request:

```go
func getProfile(c *gin.Context) {
    profile, err := grpcClient.GetProfile(c.Request.Context(), request)
    // response này phụ thuộc vào gRPC call, nên cancellation của client nên hủy call.
    _ = profile
    _ = err
}
```

Nhưng đây là lỗi phổ biến với async task:

```go
func createReport(c *gin.Context) {
    go func() {
        // Sai: handler có thể return ngay sau đó.
        // gRPC call nhận context canceled hoặc deadline exceeded giữa chừng.
        _ = grpcClient.Generate(c.Request.Context(), request)
    }()

    c.Status(http.StatusAccepted)
}
```

Nếu công việc phải tiếp tục sau khi response đã gửi, tách dữ liệu cần thiết ra khỏi request và dùng application context có timeout riêng:

```go
func createReport(c *gin.Context, appCtx context.Context) {
    reportRequest := buildReportRequest(c) // copy payload, user ID, request ID cần thiết

    go func(req *pb.GenerateRequest) {
        ctx, cancel := context.WithTimeout(appCtx, 30*time.Second)
        defer cancel()

        _ = grpcClient.Generate(ctx, req)
    }(reportRequest)

    c.Status(http.StatusAccepted)
}
```

`appCtx` nên bị cancel khi graceful shutdown, thay vì sống vô hạn bằng `context.Background()`. Với task quan trọng, production system thường gửi job vào queue/worker thay vì tạo goroutine không được theo dõi trong handler.

### `c.Copy()` giải quyết điều gì, và không giải quyết điều gì?

Gin tái sử dụng `*gin.Context` sau khi handler return. Vì vậy không được giữ hoặc truyền `c` gốc qua ranh giới goroutine; điều đó có thể gây data race, đọc nhầm dữ liệu của request khác, hoặc panic.

```go
// Sai: dùng Gin context gốc sau khi handler có thể đã return.
go func() {
    log.Println(c.Request.URL.Path)
}()
```

Nếu goroutine chỉ cần đọc metadata từ Gin, dùng bản copy chỉ-đọc:

```go
cCopy := c.Copy()
go func() {
    log.Println(cCopy.Request.URL.Path)
}()
```

Tuy nhiên, `c.Copy()` **không kéo dài lifetime của request context**. `cCopy.Request.Context()` vẫn là request context, nên vẫn có thể bị cancel sau khi handler return. `c.Copy()` bảo vệ dữ liệu của Gin context khỏi pool reuse; nó không biến request-scoped work thành background work.

### Interval và worker nền trong Gin

Không khởi động interval mỗi khi có HTTP request, cũng không dùng request context cho interval:

```go
// Sai: tạo thêm worker cho mỗi request; worker chết khi request kết thúc.
func handler(c *gin.Context) {
    go runInterval(c.Request.Context())
}
```

Khởi động worker một lần khi application boot, với application context:

```go
appCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

go runInterval(appCtx)
router.Run(":8080")
```

`runInterval` nên `select` trên `appCtx.Done()` để dừng sạch khi server shutdown.

## 15. Lỗi thường gặp

| Lỗi | Hậu quả | Cách đúng |
| --- | --- | --- |
| Đợi `<-context.Background().Done()` | Block mãi mãi. | Dùng context có cancel/timeout, hoặc dùng channel khác. |
| Tạo worker bằng `Background()` | Worker không theo server lifecycle. | Truyền `s.ctx` vào worker. |
| Chỉ `cancel()` mà không `conn.Close()` | Goroutine có thể kẹt ở network read. | Cancel context và đóng socket. |
| Đóng channel từ phía receiver | Có thể panic `send on closed channel`. | Sender/owner cuối cùng là bên đóng channel. |
| Đóng AOF trước khi đợi handlers | Handler đang append có thể gặp lỗi/mất dữ liệu. | Drain handlers trước, đóng AOF sau. |
| Giữ mutex trong lúc chờ goroutine | Dễ deadlock, block code khác. | Lấy state cần thiết, unlock, rồi mới chờ. |
| Gọi `Shutdown` hai lần không có guard | Panic do double close hoặc close file lặp lại. | Dùng `sync.Once` hoặc state + `shutdownDone`. |

## 16. Checklist khi tạo goroutine trong Go

Trước khi viết `go func()`, trả lời các câu hỏi:

1. Ai sở hữu goroutine này?
2. Điều kiện nào làm nó dừng: context, channel, socket close, hay process exit?
3. Ai gọi cancellation hoặc đóng channel?
4. Ai đợi nó kết thúc: `WaitGroup`, `done` channel, hay không cần đợi?
5. Nó có thể block ở send, receive, mutex, ticker hay network I/O không?
6. Nếu shutdown xảy ra tại thời điểm đó, có cần thêm cancellation path nào không?
7. Resource nó dùng (file, socket, map) được đóng sau khi nó đã dừng chưa?

## 17. Lệnh kiểm tra

Chạy test và race detector sau khi sửa code concurrency:

```bash
go test ./...
go test -race ./...
```

Race detector không chứng minh code không có deadlock hoặc goroutine leak, nhưng rất hữu ích để tìm data race trên shared state.

## 18. Tóm tắt kiến thức Go học được từ Phase 10

- `context.Background()` là root, không cancel được; `Done()` của nó là `nil`.
- `context.TODO()` có hành vi như `Background()`, nhưng biểu thị context đúng vẫn chưa được quyết định.
- `WithCancel`, `WithTimeout`, `WithDeadline`, `signal.NotifyContext` tạo cancellation path.
- `Done()` là tín hiệu; `Err()` cho biết lý do; context không tự động dừng goroutine.
- `select` chọn một channel operation đang ready; send/receive nằm ngay trong `case`.
- `done` channel báo hoàn tất; context `Done` yêu cầu dừng.
- Context cancellation không thay thế việc close TCP connection khi read đang block.
- Gin request context phù hợp cho request-scoped I/O; background task cần application context riêng. `c.Copy()` chỉ bảo vệ `gin.Context` khỏi pool reuse, không kéo dài request context.
- `WaitGroup`, mutex, atomic state, `sync.Once` và close channel phối hợp để shutdown đúng thứ tự, an toàn khi concurrent.
- Mỗi goroutine nên có owner và exit path rõ ràng. Đây là quy tắc quan trọng nhất của Phase 10.
