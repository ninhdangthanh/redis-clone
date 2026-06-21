# Phase 9: Pub/Sub và Go Concurrency

Tài liệu này giải thích Phase 9 của Redis clone theo code hiện tại. Mục tiêu là nắm được đường đi của một command, một Pub/Sub message, và lý do cần goroutine, channel, mutex.

## 1. Phase 9 thay đổi điều gì?

Trước Phase 9, một connection gần như có luồng tuần tự:

```text
client gửi command
  -> handleConn đọc command
  -> dispatcher xử lý Store
  -> server trả response
```

Ví dụ `GET name`:

```text
GET name -> HandleGet -> Store.Get -> RESP response
```

Pub/Sub thay đổi mô hình này. Sau khi một client chạy `SUBSCRIBE news`, server phải có thể làm hai việc cùng lúc:

1. Nhận command mới từ chính client đó, như `PING`, `UNSUBSCRIBE`, `QUIT`, hoặc `SUBSCRIBE` thêm channel.
2. Đẩy message đến client ngay khi client khác chạy `PUBLISH news hello`.

Nếu server chỉ dùng một lệnh đọc socket tuần tự, nó có thể đang chờ client A gửi command trong khi message của client B cần được đẩy ngay cho A. Phase 9 thêm concurrency để giải quyết điểm này.

## 2. Các thành phần mới

| Thành phần | File | Trách nhiệm |
| --- | --- | --- |
| `Hub` | `internal/pubsub/hub.go` | Lưu subscription và fan-out message đến subscribers. |
| `Subscriber` | `internal/pubsub/hub.go` | Đại diện cho một connection khi nó vào subscribed mode. |
| `Message` | `internal/pubsub/hub.go` | Dữ liệu Pub/Sub gồm `Channel` và `Payload`. |
| `PubSub` trong `CommandContext` | `internal/command/type.go` | Cho `PUBLISH` truy cập Hub dùng chung. |
| `HandlePublish` | `internal/command/pubsub.go` | Xử lý command `PUBLISH channel message`. |
| `readCommands` | `cmd/main.go` | Goroutine đọc command từ socket và đưa vào `cmdCh`. |
| `serveSubscribedConn` | `cmd/main.go` | Vòng đời của một connection trong subscribed mode. |

Trong `main`, chỉ một Hub được tạo và chia sẻ cho tất cả connections:

```go
pubsubHub := pubsub.NewHub()

go handleConn(conn, store, aofFile, pubsubHub)
```

Vì vậy publisher ở connection B có thể tìm subscriber ở connection A.

## 3. Dữ liệu subscription

Hub lưu subscriptions bằng:

```go
channels map[string]map[*Subscriber]struct{}
```

Đọc theo từ trong ra ngoài:

- `string`: tên channel, ví dụ `"news"`.
- `map[*Subscriber]struct{}`: tập các subscriber của channel đó.
- `*Subscriber`: con trỏ tới subscriber, để cùng một subscriber có một danh tính duy nhất trong map.
- `struct{}`: value rỗng. Go không có `Set` built-in, nên `map[T]struct{}` là cách thông dụng để tạo tập hợp.

Ví dụ:

```go
channels := map[string]map[*Subscriber]struct{}{
    "news": {
        subscriberA: {},
        subscriberB: {},
    },
    "events": {
        subscriberB: {},
    },
}
```

Ý nghĩa:

```text
news   -> A, B
events -> B
```

### Tại sao không dùng `map[string][]*Subscriber`?

Slice cần duyệt để kiểm tra duplicate khi subscribe và duyệt để tìm phần tử khi unsubscribe. Hai thao tác đó là `O(n)` theo số subscriber.

```go
// Slice: cần tự kiểm tra trùng lặp và tự xóa đúng index.
for _, existing := range channels["news"] {
    if existing == sub {
        // đã tồn tại
    }
}
```

Map-as-set cho phép:

```go
h.channels[channel][sub] = struct{}{} // subscribe, trung bình O(1)
delete(h.channels[channel], sub)       // unsubscribe, trung bình O(1)
```

Map cũng tự đảm bảo uniqueness. Gọi `SUBSCRIBE news` hai lần với cùng connection vẫn chỉ có một entry trong `h.channels["news"]`, do đó `PUBLISH news ...` chỉ gửi một message cho connection đó.

`subscribeChannels` có thể được gọi nhiều lần: một lần khi vào subscribed mode, và các lần sau nếu client đã subscribe mà gửi thêm `SUBSCRIBE`. Đây là bình thường. Gọi lại cùng channel sẽ gửi subscribe reply, nhưng không tạo subscriber trùng lặp.

## 4. `Subscriber` lưu gì?

```go
type Subscriber struct {
    ID       uint64
    Messages chan Message

    mu       sync.RWMutex
    channels map[string]struct{}
}
```

Một subscriber có hai phần state:

- `Messages`: inbox của connection. `Publish` đưa message vào đây; `serveSubscribedConn` lấy message ra và viết về socket.
- `channels`: tập channel mà chính subscriber đang theo dõi. Nó dùng để tính subscription count và xử lý `UNSUBSCRIBE` không có argument.

Hub giữ chiều `channel -> subscribers`; Subscriber giữ chiều ngược lại `subscriber -> channels`. Lưu cả hai chiều làm `Publish`, `UNSUBSCRIBE`, và `UNSUBSCRIBE` all thực hiện rõ ràng và hiệu quả.

## 5. Hai channel khác nhau trong Phase 9

Đây là điểm quan trọng nhất:

```text
cmdCh        : command từ client -> code xử lý connection
sub.Messages : message từ Hub    -> code xử lý subscribed connection
```

`cmdCh` có kiểu:

```go
<-chan parsedCommand
```

Nó chuyển các command như `GET`, `SUBSCRIBE`, `PING`, `UNSUBSCRIBE`, `QUIT`.

`sub.Messages` có kiểu:

```go
chan pubsub.Message
```

Nó chỉ chuyển Pub/Sub message do client khác publish, ví dụ `PUBLISH news hello`.

`PING` trong subscribed mode đi qua `cmdCh`, không đi qua `sub.Messages`. Ngược lại, message từ `PUBLISH` không đi qua `readCommands` của subscriber.

## 6. `readCommands`: goroutine đọc socket

Code hiện tại:

```go
func readCommands(r *bufio.Reader) <-chan parsedCommand {
    ch := make(chan parsedCommand, 1)
    go func() {
        defer close(ch)
        for {
            parsed := readRESPCommand(r)
            ch <- parsed
            if parsed.err != nil {
                return
            }
        }
    }()
    return ch
}
```

Hàm này chạy một goroutine riêng cho mỗi client connection.

### Ý nghĩa từng dòng

```go
ch := make(chan parsedCommand, 1)
```

Tạo channel có buffer size 1. Goroutine đọc có thể đưa một command vào hàng chờ trước khi consumer lấy nó. Nếu channel đã đầy, `ch <- parsed` sẽ chờ consumer đọc bớt.

```go
go func() { ... }()
```

Chạy function ẩn danh ở goroutine khác. `handleConn` và goroutine này chạy song song.

```go
parsed := readRESPCommand(r)
```

Chờ và đọc một RESP command từ socket, sau đó parse thành `parsedCommand`.

```go
ch <- parsed
```

Gửi command đã parse sang nơi xử lý.

```go
defer close(ch)
```

Khi goroutine return, channel được đóng. Consumer có thể nhận `ok == false` khi đọc từ channel đã đóng và không còn item nào.

### Goroutine này dừng khi nào?

Nó dừng khi `readRESPCommand` trả lỗi, thường do:

- client đóng connection;
- client gửi `QUIT`, `handleConn` return và `defer conn.Close()` đóng socket;
- network error;
- client gửi RESP dang dở rồi đóng socket.

Khi có lỗi, code gửi `parsedCommand` chứa `err` vào `cmdCh` trước, sau đó return và đóng channel. Nơi xử lý thấy `parsed.err != nil` thì return khỏi connection.

Nó không dừng chỉ vì client `PING`, `SUBSCRIBE`, hoặc `UNSUBSCRIBE`. Sau `UNSUBSCRIBE` hết channel, connection trở lại normal mode và cùng goroutine này vẫn đọc command tiếp trên socket cũ.

### Debug print hiện tại

Nếu trong goroutine có:

```go
fmt.Println("Pass new command through subscriber mode by channel")
```

nó sẽ in với mọi command đọc được từ socket, cả trước và sau khi subscribe. Tên log dễ gây hiểu nhầm vì goroutine này không biết connection đang ở subscribed mode hay không. Tên rõ ràng hơn là:

```text
Read client command and sent it to cmdCh
```

## 7. Normal mode và subscribed mode dùng chung `cmdCh`

`handleConn` tạo `cmdCh` một lần:

```go
cmdCh := readCommands(r)
```

Trước subscribe, outer loop của `handleConn` là consumer:

```go
for {
    parsed, ok := <-cmdCh
    // dispatch command bình thường
}
```

Khi nhận `SUBSCRIBE`, nó gọi:

```go
serveSubscribedConn(cmdCh, commandContext.CommandContext, args)
```

Outer loop không tiếp tục ngay. Nó tạm dừng trong lời gọi `serveSubscribedConn`. Trong lúc đó, `serveSubscribedConn` trở thành consumer của cùng `cmdCh`.

```text
Trước SUBSCRIBE:
socket -> readCommands goroutine -> cmdCh -> handleConn outer loop

Trong subscribed mode:
socket -> readCommands goroutine -> cmdCh -> serveSubscribedConn
```

Hai nơi không đọc `cmdCh` cùng lúc, vì outer loop đang chờ `serveSubscribedConn` return.

Khi `UNSUBSCRIBE` làm subscription count về 0, `serveSubscribedConn` return `false`. `handleConn` chạy `continue`, quay lại outer loop và lại đọc từ `cmdCh` như normal mode.

## 8. Luồng `SUBSCRIBE`

Ví dụ client A gửi:

```text
SUBSCRIBE news
```

Luồng chạy theo thứ tự:

```text
1. readCommands đọc RESP command từ socket A.
2. readCommands gửi parsedCommand vào cmdCh.
3. handleConn outer loop nhận command từ cmdCh.
4. handleConn gọi serveSubscribedConn.
5. serveSubscribedConn tạo Subscriber mới.
6. subscribeChannels gọi Hub.Subscribe(sub, "news").
7. Hub thêm sub vào channels["news"] và thêm "news" vào sub.channels.
8. Server gửi RESP reply ["subscribe", "news", 1].
9. serveSubscribedConn vào vòng select để chờ command mới hoặc Pub/Sub message.
```

## 9. Luồng `PUBLISH`

Ví dụ client B gửi:

```text
PUBLISH news hello
```

Client B vẫn ở normal mode nên command đi qua dispatcher:

```text
client B -> readCommands -> handleConn -> dispatcher -> HandlePublish -> Hub.Publish
```

`Hub.Publish`:

```go
func (h *Hub) Publish(channel string, payload []byte) int {
    h.mu.RLock()
    subscribers := make([]*Subscriber, 0, len(h.channels[channel]))
    for sub := range h.channels[channel] {
        subscribers = append(subscribers, sub)
    }
    h.mu.RUnlock()

    msg := Message{Channel: channel, Payload: append([]byte(nil), payload...)}
    for _, sub := range subscribers {
        sub.Messages <- msg
    }
    return len(subscribers)
}
```

Luồng chạy:

```text
1. Lấy RLock để đọc tập subscriber của channel an toàn.
2. Copy subscriber từ map sang slice.
3. Nhả lock.
4. Copy payload vào Message.
5. Gửi message vào Messages của từng subscriber.
6. Trả về số subscriber trong snapshot.
```

Sau đó, ở client A:

```go
case msg := <-sub.Messages:
    command.WritePubSubMessage(ctx.Writer, msg)
```

Server viết RESP message:

```text
["message", "news", "hello"]
```

Publisher B nhận integer bằng số người trong snapshot, ví dụ `(integer) 1`.

### Tại sao copy subscriber rồi mới nhả lock?

Không nên giữ `h.mu.RLock()` trong lúc gửi:

```go
sub.Messages <- msg
```

Lệnh gửi có thể block khi subscriber chậm và buffer đầy. Nếu giữ lock, `SUBSCRIBE`, `UNSUBSCRIBE`, và các thao tác Hub khác có thể bị kẹt. Copy sang slice tạo snapshot ngắn, nhả lock sớm, rồi mới fan-out.

### Tại sao copy `payload`?

```go
append([]byte(nil), payload...)
```

Tạo slice byte mới. Không copy thì caller có thể tái sử dụng và sửa mảng byte gốc trong khi subscriber chưa đọc message, làm payload của message bị thay đổi ngoài ý muốn.

## 10. `select`: chờ hai nguồn sự kiện

Trong subscribed mode:

```go
select {
case parsed, ok := <-cmdCh:
    // command mới từ chính client: PING, UNSUBSCRIBE, QUIT...
case msg := <-sub.Messages:
    // message do connection khác PUBLISH
}
```

`select` block cho tới khi ít nhất một case sẵn sàng. Đây là lý do subscriber vẫn nhận message dù không tự gõ command mới, đồng thời vẫn có thể `PING` hay `UNSUBSCRIBE` bất kỳ lúc nào.

Trong subscribed mode chỉ những command sau được chấp nhận:

```text
SUBSCRIBE
UNSUBSCRIBE
PING
QUIT
```

Command khác, như `GET` hay `SET`, nhận:

```text
ERR Can't execute command in subscribed mode
```

## 11. Luồng `UNSUBSCRIBE` và quay lại normal mode

Ví dụ subscriber chỉ đang theo dõi `news` và gửi:

```text
UNSUBSCRIBE news
```

Luồng chạy:

```text
1. readCommands đọc UNSUBSCRIBE và gửi vào cmdCh.
2. serveSubscribedConn nhận command ở case cmdCh.
3. unsubscribeChannels gọi Hub.Unsubscribe.
4. Hub xóa sub khỏi h.channels["news"] và xóa "news" khỏi sub.channels.
5. WriteSubscribeReply gửi ["unsubscribe", "news", 0].
6. Vòng for SubscriptionCount(sub) > 0 kết thúc.
7. defer UnsubscribeAll chạy để đảm bảo cleanup.
8. serveSubscribedConn return false.
9. handleConn quay lại outer loop normal mode.
```

Vì vậy nếu sau đó `GET ninh` hoặc `SET ninh hello` chạy được thì server đã thoát subscribed mode đúng cách. Prompt `(subscribed mode)>` của `redis-cli` có thể vẫn hiện; đó là UI state của client, không phải state thực tế của server. Response `GET`/`SET` mới là bằng chứng của server state.

## 12. Mutex và race condition

Nhiều goroutine có thể gọi `Publish`, `Subscribe`, và `Unsubscribe` cùng lúc trên cùng Hub. Go map không an toàn khi đọc/ghi đồng thời, nên Hub cần:

```go
mu sync.RWMutex
```

- `Lock` / `Unlock`: khi sửa `h.channels`, như subscribe và unsubscribe.
- `RLock` / `RUnlock`: khi đọc danh sách subscriber trong publish.

`Subscriber` có mutex riêng bảo vệ `sub.channels`, vì một subscriber có thể đang unsubscribe trong khi code khác cần tính subscription count hoặc lấy danh sách channels.

Nguyên tắc đang áp dụng trong Phase 9:

```text
Giữ lock ngắn nhất có thể.
Không gửi qua channel khi đang giữ Hub lock.
```

## 13. Vòng đời connection và cleanup

`serveSubscribedConn` có:

```go
defer ctx.PubSub.UnsubscribeAll(sub)
```

Nếu client disconnect hoặc `QUIT` khi vẫn subscribe nhiều channel, defer này xóa subscriber khỏi tất cả channel. Điều này tránh Hub giữ con trỏ tới connection đã chết.

`Subscriber.Messages` không bị close trong code hiện tại. Cách này tránh race `send on closed channel`: một publisher có thể đã copy subscriber vào snapshot và đang gửi message trong lúc connection khác unsubscribe/disconnect.

## 14. Giới hạn hiện tại so với Redis thật

Implementation này đúng cho Phase 9, nhưng Redis thật phức tạp hơn:

- `sub.Messages` là buffered channel 256. Nếu client rất chậm và buffer đầy, `Publish` có thể block ở `sub.Messages <- msg`.
- Redis thật có output-buffer limit và có thể disconnect subscriber chậm thay vì để publisher chờ vô hạn.
- Snapshot trong `Publish` có nghĩa một subscriber vừa unsubscribe cùng lúc với publish có thể vẫn nhận message đã nằm trong snapshot. Đây là hệ quả bình thường của concurrent snapshot.
- Pub/Sub hiện tại là runtime-only: không ghi vào `Store` và không replay từ AOF sau restart.
- Chưa có pattern subscription (`PSUBSCRIBE`) hay lệnh thống kê Pub/Sub.

## 15. Cách test thủ công bằng `redis-cli`

Terminal 1:

```bash
make run
```

Terminal 2, subscriber A:

```bash
redis-cli -p 6379 --raw
SUBSCRIBE news
```

Terminal 3, publisher:

```bash
redis-cli -p 6379
PUBLISH news "hello from publisher"
```

Publisher phải nhận `(integer) 1`; subscriber phải nhận message trên channel `news`.

Test thêm:

```text
Mở subscriber B -> SUBSCRIBE news
PUBLISH news hello -> (integer) 2 và cả A/B đều nhận
UNSUBSCRIBE news ở A
PUBLISH news hello -> chỉ B nhận
GET key trong khi A vẫn subscribe -> error subscribed mode
UNSUBSCRIBE hết channel của A
GET key / SET key value ở A -> phải chạy được normal mode
Restart server -> subscriptions cũ biến mất
```

## 16. Câu hỏi tự kiểm tra

1. Command `PING` trong subscribed mode đi qua channel nào? `cmdCh`.
2. Message từ `PUBLISH` đi qua channel nào? `sub.Messages`.
3. Tại sao `readCommands` phải là goroutine riêng? Để subscribed connection có thể chờ command và Pub/Sub message đồng thời qua `select`.
4. Khi nào outer loop của `handleConn` đọc `cmdCh`? Trước subscribe và sau khi unsubscribe hết channel.
5. Khi nào `serveSubscribedConn` đọc `cmdCh`? Trong khi subscription count lớn hơn 0.
6. Tại sao `map[*Subscriber]struct{}` tốt hơn slice ở đây? Uniqueness và subscribe/unsubscribe trung bình `O(1)`.
7. Tại sao không giữ `h.mu` khi gửi vào `sub.Messages`? Subscriber chậm có thể làm send block và khóa các thao tác Hub khác.
8. Tại sao `GET` chạy được sau unsubscribe cuối? `serveSubscribedConn` đã return, connection đã trở lại normal mode.
