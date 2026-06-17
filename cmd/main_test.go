package main

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"redis-clone/internal/aof"
	"redis-clone/internal/store"
)

func TestReplayAOFRestoresAbsoluteExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appendonly.aof")
	a, err := aof.Open(path, aof.FsyncNever)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer a.Close()

	expiresAt := time.Now().Add(1500 * time.Millisecond).UnixMilli()
	if err := a.Append([]string{"SET", "key", "value", "PXAT", strconv.FormatInt(expiresAt, 10)}); err != nil {
		t.Fatalf("Append SET returned error: %v", err)
	}

	s := store.NewStore()
	if err := ReplayAOF(s, a); err != nil {
		t.Fatalf("ReplayAOF returned error: %v", err)
	}

	ttl := s.TTL("key")
	if ttl < 0 || ttl > 1 {
		t.Fatalf("restored TTL = %d, want between 0 and 1 second", ttl)
	}
}
