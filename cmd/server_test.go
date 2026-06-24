package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"path/filepath"
	"redis-clone/internal/aof"
	"redis-clone/internal/pubsub"
	"redis-clone/internal/store"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestShutdownReleasesPortAndClosesIdleClient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("network listeners are unavailable in this environment: %v", err)
		}
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()

	aofFile, err := aof.Open(filepath.Join(t.TempDir(), "appendonly.aof"), aof.FsyncEverySec)
	if err != nil {
		t.Fatalf("open aof: %v", err)
	}
	server := NewServer(listener, store.NewStore(), aofFile, pubsub.NewHub())
	server.StartWorkers()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve() }()

	client, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer client.Close()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-serveDone; err != nil {
		t.Fatalf("serve: %v", err)
	}

	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("idle client remained open after shutdown")
	}

	rebound, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port was not released after shutdown: %v", err)
	}
	defer rebound.Close()

	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
}

func TestCommandExecutorSerializesConcurrentClientCommands(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	aofFile, err := aof.Open(filepath.Join(t.TempDir(), "appendonly.aof"), aof.FsyncNever)
	if err != nil {
		t.Fatalf("open aof: %v", err)
	}
	defer aofFile.Close()

	executor := NewCommandExecutor(ctx, store.NewStore(), aofFile, pubsub.NewHub())
	defer func() {
		cancel()
		if err := executor.Stop(context.Background()); err != nil {
			t.Fatalf("stop executor: %v", err)
		}
	}()

	const clients = 20
	results := make(chan string, clients)
	for i := 0; i < clients; i++ {
		go func(i int) {
			var output bytes.Buffer
			writer := bufio.NewWriter(&output)
			_, ok := executor.Submit(ctx, writer, []string{"SET", "client:" + strconv.Itoa(i), "value"})
			if !ok {
				results <- "executor stopped"
				return
			}
			results <- output.String()
		}(i)
	}

	for i := 0; i < clients; i++ {
		if got := <-results; got != "+OK\r\n" {
			t.Fatalf("unexpected SET response: %q", got)
		}
	}
}
