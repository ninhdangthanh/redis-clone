package store

import (
	"errors"
	"testing"
	"time"
)

func TestTTLStates(t *testing.T) {
	s := NewStore()

	if got := s.TTL("missing"); got != -2 {
		t.Fatalf("TTL missing = %d, want -2", got)
	}

	s.Set("plain", []byte("value"), 0)
	if got := s.TTL("plain"); got != -1 {
		t.Fatalf("TTL without expiry = %d, want -1", got)
	}

	s.Set("short", []byte("value"), 1)
	time.Sleep(2 * time.Millisecond)
	if got := s.TTL("short"); got != -2 {
		t.Fatalf("TTL expired = %d, want -2", got)
	}
	if _, ok := s.Get("short"); ok {
		t.Fatal("expired key should not be readable")
	}
}

func TestNoEvictionRejectsWritesOverMaxMemory(t *testing.T) {
	s := NewStoreWithConfig(Config{MaxMemory: 80, EvictionPolicy: PolicyNoEviction})

	err := s.Set("too-big", []byte("this value is larger than the tiny configured memory limit"), 0)
	if !errors.Is(err, ErrMaxMemory) {
		t.Fatalf("SET over maxmemory err = %v, want ErrMaxMemory", err)
	}
	if s.Len() != 0 {
		t.Fatalf("rejected write should be rolled back, len = %d", s.Len())
	}
}

func TestAllKeysLRUEvictsLeastRecentlyUsedKey(t *testing.T) {
	s := NewStoreWithConfig(Config{MaxMemory: 160, EvictionPolicy: PolicyAllKeysLRU})

	if err := s.Set("old", []byte("1234567890"), 0); err != nil {
		t.Fatalf("set old: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := s.Set("fresh", []byte("1234567890"), 0); err != nil {
		t.Fatalf("set fresh: %v", err)
	}
	time.Sleep(time.Millisecond)
	if _, ok := s.Get("fresh"); !ok {
		t.Fatal("fresh should be readable before eviction")
	}
	if err := s.Set("new", []byte("1234567890"), 0); err != nil {
		t.Fatalf("set new: %v", err)
	}

	if _, ok := s.Get("old"); ok {
		t.Fatal("old should have been evicted")
	}
	if _, ok := s.Get("fresh"); !ok {
		t.Fatal("fresh should remain after LRU eviction")
	}
}

func TestEvictionPolicyRejectsSingleValueThatCannotFit(t *testing.T) {
	s := NewStoreWithConfig(Config{MaxMemory: 90, EvictionPolicy: PolicyAllKeysLRU})

	if err := s.Set("small", []byte("ok"), 0); err != nil {
		t.Fatalf("set small: %v", err)
	}
	err := s.Set("huge", []byte("this value cannot fit even after every other key is evicted"), 0)
	if !errors.Is(err, ErrMaxMemory) {
		t.Fatalf("oversized SET err = %v, want ErrMaxMemory", err)
	}
	if _, ok := s.Get("huge"); ok {
		t.Fatal("oversized key should not be written")
	}
}

func TestAllKeysLFUEvictsLeastFrequentlyUsedKey(t *testing.T) {
	s := NewStoreWithConfig(Config{MaxMemory: 160, EvictionPolicy: PolicyAllKeysLFU})

	if err := s.Set("rare", []byte("1234567890"), 0); err != nil {
		t.Fatalf("set rare: %v", err)
	}
	if err := s.Set("often", []byte("1234567890"), 0); err != nil {
		t.Fatalf("set often: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, ok := s.Get("often"); !ok {
			t.Fatal("often should be readable before eviction")
		}
	}
	if err := s.Set("new", []byte("1234567890"), 0); err != nil {
		t.Fatalf("set new: %v", err)
	}

	if _, ok := s.Get("rare"); ok {
		t.Fatal("rare should have been evicted")
	}
	if _, ok := s.Get("often"); !ok {
		t.Fatal("often should remain after LFU eviction")
	}
}

func TestVolatileLRUOnlyEvictsExpiringKeys(t *testing.T) {
	s := NewStoreWithConfig(Config{MaxMemory: 170, EvictionPolicy: PolicyVolatileLRU})

	if err := s.Set("volatile", []byte("1234567890"), 60_000); err != nil {
		t.Fatalf("set volatile: %v", err)
	}
	if err := s.Set("persistent", []byte("1234567890"), 0); err != nil {
		t.Fatalf("set persistent: %v", err)
	}
	if err := s.Set("new", []byte("1234567890"), 0); err != nil {
		t.Fatalf("set new: %v", err)
	}

	if _, ok := s.Get("volatile"); ok {
		t.Fatal("volatile key should have been evicted first")
	}
	if _, ok := s.Get("persistent"); !ok {
		t.Fatal("persistent key should remain under volatile-lru")
	}
}
