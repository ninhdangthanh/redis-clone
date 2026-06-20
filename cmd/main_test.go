package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

func TestSeedData(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Skipf("Redis clone is not running at %s: %v", addr, err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	// Keep this seed repeatable without modifying application keys.
	sendRESPCommand(t, conn, reader, "DEL",
		"seed:user:1:name", "seed:user:1:email", "seed:user:1:status", "seed:user:1:city",
		"seed:user:1:roles", "seed:user:1:tags", "seed:user:1:profile",
		"seed:user:2:name", "seed:user:2:email", "seed:user:2:status", "seed:user:2:roles", "seed:user:2:tags", "seed:user:2:profile",
		"seed:product:1:name", "seed:product:1:price", "seed:product:1:categories", "seed:product:1:details",
	)

	seedCommands := [][]string{
		{"SET", "seed:user:1:name", "Alice"},
		{"SET", "seed:user:1:email", "alice@example.com"},
		{"SET", "seed:user:1:status", "active"},
		{"SET", "seed:user:1:city", "Ho Chi Minh City"},
		{"RPUSH", "seed:user:1:roles", "admin", "editor"},
		{"SADD", "seed:user:1:tags", "golang", "redis"},
		{"HSET", "seed:user:1:profile", "email", "alice@example.com"},
		{"HSET", "seed:user:1:profile", "joined_at", "2026-06-20"},
		{"SET", "seed:user:2:name", "Bob"},
		{"SET", "seed:user:2:email", "bob@example.com"},
		{"SET", "seed:user:2:status", "inactive"},
		{"RPUSH", "seed:user:2:roles", "viewer"},
		{"SADD", "seed:user:2:tags", "go", "docker", "redis"},
		{"HSET", "seed:user:2:profile", "email", "bob@example.com"},
		{"HSET", "seed:user:2:profile", "joined_at", "2026-05-10"},
		{"SET", "seed:product:1:name", "Redis Handbook"},
		{"SET", "seed:product:1:price", "29.99"},
		{"SADD", "seed:product:1:categories", "books", "database", "programming"},
		{"HSET", "seed:product:1:details", "stock", "42"},
		{"HSET", "seed:product:1:details", "currency", "USD"},
	}
	for _, args := range seedCommands {
		sendRESPCommand(t, conn, reader, args...)
	}
}

func sendRESPCommand(t *testing.T, conn net.Conn, reader *bufio.Reader, args ...string) {
	t.Helper()
	if _, err := fmt.Fprintf(conn, "*%d\r\n", len(args)); err != nil {
		t.Fatalf("write command header: %v", err)
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			t.Fatalf("write command argument: %v", err)
		}
	}

	response, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read command response: %v", err)
	}
	_ = response
}
