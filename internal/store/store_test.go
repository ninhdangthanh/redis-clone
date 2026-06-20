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

func TestInfoReportsMetricsConfigAndState(t *testing.T) {
	s := NewStoreWithConfig(Config{MaxMemory: 1024, EvictionPolicy: PolicyAllKeysLRU})
	if err := s.Set("string", []byte("value"), 60_000); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if _, err := s.RPush("list", []byte("one")); err != nil {
		t.Fatalf("RPush returned error: %v", err)
	}
	if _, err := s.SAdd("set", []byte("one")); err != nil {
		t.Fatalf("SAdd returned error: %v", err)
	}
	if _, err := s.HSet("hash", "field", []byte("value")); err != nil {
		t.Fatalf("HSet returned error: %v", err)
	}

	info := s.Info()
	if info.Config.MaxMemory != 1024 || info.Config.EvictionPolicy != PolicyAllKeysLRU {
		t.Fatalf("config = %+v, want maxmemory 1024 and allkeys-lru", info.Config)
	}
	if info.Metrics.KeyCount != 4 || info.Metrics.ExpiringKeys != 1 || info.Metrics.MemoryUsageBytes <= 0 {
		t.Fatalf("metrics = %+v, want 4 keys, 1 expiring key, and memory usage", info.Metrics)
	}
	if info.State != (State{StringKeys: 1, ListKeys: 1, SetKeys: 1, HashKeys: 1}) {
		t.Fatalf("state = %+v, want one key of each type", info.State)
	}
}

func TestExpiredKeyCanChangeType(t *testing.T) {
	s := NewStore()
	s.Set("key", []byte("value"), 1)
	time.Sleep(2 * time.Millisecond)

	added, err := s.SAdd("key", []byte("member"))
	if err != nil || !added {
		t.Fatalf("SAdd after string expiry = added %v err %v, want true nil", added, err)
	}
}

func TestListPushesReturnLengthAndWrongType(t *testing.T) {
	s := NewStore()

	if got, err := s.LPush("list", []byte("a"), []byte("b")); err != nil || got != 2 {
		t.Fatalf("LPush length = %d err = %v, want 2 nil", got, err)
	}

	values, ok := s.LRange("list", 0, -1)
	if !ok || len(values) != 2 || string(values[0]) != "b" || string(values[1]) != "a" {
		t.Fatalf("LPush order = %q ok = %v, want [b a] true", values, ok)
	}

	s.Set("string", []byte("value"), 0)
	if _, err := s.RPush("string", []byte("x")); !errors.Is(err, ErrWrongType) {
		t.Fatal("RPush on string key should report wrong type")
	}
}

func TestStringValuesAreCopied(t *testing.T) {
	s := NewStore()
	in := []byte("value")
	s.Set("key", in, 0)
	in[0] = 'X'

	got, ok := s.Get("key")
	if !ok || string(got) != "value" {
		t.Fatalf("stored value = %q ok = %v, want value true", got, ok)
	}

	got[0] = 'Y'
	gotAgain, ok := s.Get("key")
	if !ok || string(gotAgain) != "value" {
		t.Fatalf("stored value after caller mutation = %q ok = %v, want value true", gotAgain, ok)
	}
}

func TestLRangeNormalizesBounds(t *testing.T) {
	s := NewStore()
	if _, err := s.RPush("list", []byte("a"), []byte("b"), []byte("c")); err != nil {
		t.Fatalf("RPush returned error: %v", err)
	}

	values, ok := s.LRange("list", -2, 99)
	if !ok || len(values) != 2 || string(values[0]) != "b" || string(values[1]) != "c" {
		t.Fatalf("LRange(-2, 99) = %q ok = %v, want [b c] true", values, ok)
	}

	values, ok = s.LRange("list", 5, 10)
	if !ok || len(values) != 0 {
		t.Fatalf("LRange out of range = %q ok = %v, want empty true", values, ok)
	}
}

func TestSetAndHashOperations(t *testing.T) {
	s := NewStore()

	added, err := s.SAdd("set", []byte("a"))
	if err != nil || !added {
		t.Fatalf("first SAdd = added %v err %v, want true nil", added, err)
	}
	added, err = s.SAdd("set", []byte("a"))
	if err != nil || added {
		t.Fatalf("duplicate SAdd = added %v err %v, want false nil", added, err)
	}
	members, ok := s.SMembers("set")
	if !ok || len(members) != 1 || string(members[0]) != "a" {
		t.Fatalf("SMembers = %q ok = %v, want [a] true", members, ok)
	}

	created, err := s.HSet("hash", "field", []byte("one"))
	if err != nil || created != 1 {
		t.Fatalf("first HSet = %d err = %v, want 1 nil", created, err)
	}
	created, err = s.HSet("hash", "field", []byte("two"))
	if err != nil || created != 0 {
		t.Fatalf("update HSet = %d err = %v, want 0 nil", created, err)
	}
	val, ok := s.HGet("hash", "field")
	if !ok || string(val) != "two" {
		t.Fatalf("HGet = %q ok = %v, want two true", val, ok)
	}
}

func TestWrongTypeForSetAndHash(t *testing.T) {
	s := NewStore()
	s.Set("key", []byte("value"), 0)

	if _, err := s.SAdd("key", []byte("member")); !errors.Is(err, ErrWrongType) {
		t.Fatal("SAdd on string key should report wrong type")
	}
	if _, err := s.HSet("key", "field", []byte("value")); !errors.Is(err, ErrWrongType) {
		t.Fatal("HSet on string key should report wrong type")
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
