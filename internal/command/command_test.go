package command

import (
	"bufio"
	"bytes"
	"strconv"
	"testing"
	"time"

	"redis-clone/internal/store"
)

func newCommandTestContext() (*CommandContext, *bytes.Buffer) {
	var buf bytes.Buffer
	return &CommandContext{
		Writer:        NewRespWriter(bufio.NewWriter(&buf)),
		Store:         store.NewStore(),
		Authenticated: true,
	}, &buf
}

func TestBasicCommands(t *testing.T) {
	tests := []struct {
		name string
		run  func(ctx *CommandContext) bool
		want string
	}{
		{
			name: "ping default",
			run:  func(ctx *CommandContext) bool { return HandlePing(ctx, []string{"PING"}) },
			want: "+PONG\r\n",
		},
		{
			name: "echo",
			run:  func(ctx *CommandContext) bool { return HandleEcho(ctx, []string{"ECHO", "hello"}) },
			want: "$5\r\nhello\r\n",
		},
		{
			name: "quit",
			run:  func(ctx *CommandContext) bool { return HandleQuit(ctx, []string{"QUIT"}) },
			want: "+OK\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, buf := newCommandTestContext()
			if !tt.run(ctx) {
				t.Fatal("command returned false")
			}
			if got := buf.String(); got != tt.want {
				t.Fatalf("response = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStringCommandsSetGetDel(t *testing.T) {
	ctx, buf := newCommandTestContext()

	if !HandleSet(ctx, []string{"SET", "name", "redis"}) {
		t.Fatal("SET returned false")
	}
	if got := buf.String(); got != "+OK\r\n" {
		t.Fatalf("SET response = %q, want +OK", got)
	}

	buf.Reset()
	if !HandleGet(ctx, []string{"GET", "name"}) {
		t.Fatal("GET returned false")
	}
	if got := buf.String(); got != "$5\r\nredis\r\n" {
		t.Fatalf("GET response = %q, want redis bulk string", got)
	}

	buf.Reset()
	if !HandleDel(ctx, []string{"DEL", "name"}) {
		t.Fatal("DEL returned false")
	}
	if got := buf.String(); got != ":1\r\n" {
		t.Fatalf("DEL response = %q, want :1", got)
	}
}

func TestSetWithPXExpires(t *testing.T) {
	ctx, buf := newCommandTestContext()

	if !HandleSet(ctx, []string{"SET", "temp", "value", "PX", "1"}) {
		t.Fatal("SET PX returned false")
	}
	time.Sleep(2 * time.Millisecond)

	buf.Reset()
	if !HandleGet(ctx, []string{"GET", "temp"}) {
		t.Fatal("GET returned false")
	}
	if got := buf.String(); got != "$-1\r\n" {
		t.Fatalf("expired GET response = %q, want null bulk", got)
	}
}

func TestSetWithPXATUsesAbsoluteExpiry(t *testing.T) {
	ctx, buf := newCommandTestContext()
	expiresAt := time.Now().Add(1500 * time.Millisecond).UnixMilli()

	if !HandleSet(ctx, []string{"SET", "temp", "value", "PXAT", strconv.FormatInt(expiresAt, 10)}) {
		t.Fatal("SET PXAT returned false")
	}
	if got := buf.String(); got != "+OK\r\n" {
		t.Fatalf("SET PXAT response = %q, want +OK", got)
	}

	ttl := ctx.Store.TTL("temp")
	if ttl < 0 || ttl > 1 {
		t.Fatalf("SET PXAT TTL = %d, want between 0 and 1 second", ttl)
	}
}

func TestPExpireAtCommand(t *testing.T) {
	ctx, buf := newCommandTestContext()
	ctx.Store.Set("temp", []byte("value"), 0)
	expiresAt := time.Now().Add(1500 * time.Millisecond).UnixMilli()

	if !HandleTTLCommands(ctx, []string{"PEXPIREAT", "temp", strconv.FormatInt(expiresAt, 10)}) {
		t.Fatal("PEXPIREAT returned false")
	}
	if got := buf.String(); got != ":1\r\n" {
		t.Fatalf("PEXPIREAT response = %q, want :1", got)
	}

	ttl := ctx.Store.TTL("temp")
	if ttl < 0 || ttl > 1 {
		t.Fatalf("PEXPIREAT TTL = %d, want between 0 and 1 second", ttl)
	}
}

func TestListCommandResponses(t *testing.T) {
	ctx, buf := newCommandTestContext()

	if !HandleListCommands(ctx, []string{"LPUSH", "items", "a", "b"}) {
		t.Fatal("LPUSH returned false")
	}
	if got := buf.String(); got != ":2\r\n" {
		t.Fatalf("LPUSH response = %q, want :2", got)
	}

	buf.Reset()
	if !HandleListCommands(ctx, []string{"LRANGE", "items", "0", "-1"}) {
		t.Fatal("LRANGE returned false")
	}
	if got := buf.String(); got != "*2\r\n$1\r\nb\r\n$1\r\na\r\n" {
		t.Fatalf("LRANGE response = %q, want [b a]", got)
	}
}

func TestWrongTypeCommandFails(t *testing.T) {
	ctx, buf := newCommandTestContext()
	ctx.Store.Set("key", []byte("value"), 0)

	if HandleListCommands(ctx, []string{"RPUSH", "key", "x"}) {
		t.Fatal("RPUSH against string key should fail")
	}
	if got := buf.String(); got != "-WRONGTYPE Operation against a key holding the wrong kind of value\r\n" {
		t.Fatalf("wrong-type response = %q", got)
	}
}

func TestUnauthenticatedWriteCommandFails(t *testing.T) {
	ctx, buf := newCommandTestContext()
	ctx.Authenticated = false

	if HandleStringCommands(ctx, []string{"SET", "key", "value"}) {
		t.Fatal("unauthenticated SET should fail")
	}
	if got := buf.String(); got != "-NOAUTH Authentication required\r\n" {
		t.Fatalf("NOAUTH response = %q", got)
	}
}
