# Kế hoạch event loop một lệnh

## Mục tiêu

Cho các lệnh Redis thông thường chạy trong một vòng lặp do server sở hữu.
Nhiều client vẫn có thể đọc và ghi TCP socket đồng thời, nhưng chỉ event loop
thực thi các handler lệnh của client thông thường. TTL và eviction vẫn là các
worker nền, được bảo vệ bởi mutex của Store.

## Thiết kế

```text
clients (nhiều kết nối TCP)
        |
        v
goroutine đọc theo từng kết nối
        |
        v
command queue có giới hạn (256 request)
        |
        v
1 goroutine CommandExecutor
  - dispatcher
  - truy cập Store
  - append AOF
  - ghi / flush response
```

Mỗi kết nối gửi một lệnh và đợi lệnh đó hoàn tất trước khi đọc lệnh kế tiếp.
Như vậy thứ tự response của các request pipelining vẫn được giữ nguyên, còn
queue có giới hạn sẽ tạo backpressure khi tải cao.

## Các giai đoạn triển khai

- [x] Thêm `CommandExecutor`, với một request queue và một goroutine thực thi.
- [x] Đi các lệnh thông thường như `GET`, `SET`, collections, TTL, `PUBLISH`,
  `INFO`, và `QUIT` qua executor này.
- [x] Khởi động executor cùng server context và chờ nó khi shutdown.
- [x] Giữ việc đọc socket chạy đồng thời; đây là phần chờ I/O, không phải phần
  thực thi database.
- [x] Giữ các kết nối đang subscribe ở luồng ghi message riêng của chúng.
  `PUBLISH` từ client thông thường vẫn đi qua event loop; phần bookkeeping của
  subscription vẫn được bảo vệ bởi mutex của Pub/Sub hub.
- [x] Thêm coverage với 20 client đồng thời gửi lệnh.
- [ ] Quyết định có giữ mutex của Store hay không. Nó vẫn hữu ích cho TTL và
  eviction worker; muốn bỏ an toàn thì các worker đó cũng phải submit công việc
  vào event loop.

## Không đặt mục tiêu

Phần này không cố sao chép chính xác cách Redis triển khai networking. Go vẫn
dùng goroutine để accept client và chờ đọc socket. Tính chất một luồng quan
trọng ở đây là các command handler truy cập database theo một chuỗi xác định,
duy nhất.
