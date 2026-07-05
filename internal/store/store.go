package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

var (
	ErrMaxMemory = errors.New("maxmemory limit reached")
	ErrWrongType = errors.New("wrong value type")
)

type ValueType int

const (
	StringType ValueType = iota
	ListType
	SetType
	HashType
)

type Value struct {
	Type ValueType // kiểu dữ liệu, quyết định trường nào bên dưới được dùng
	Str  []byte    // dữ liệu cho StringType
	List [][]byte  // dữ liệu cho ListType
	Set  map[string]struct{}
	Hash map[string][]byte

	// TTL: thời điểm hết hạn (Unix nanoseconds). 0 = không hết hạn.
	// Dùng để xoá key khi hết hạn và cho policy volatile-* (chỉ evict key có TTL).
	ExpiresAt int64

	CreatedAt    int64  // thời điểm tạo (Unix ns); hiện chỉ được ghi, chưa đọc ở đâu — metadata để dành (age/OBJECT IDLETIME/debug)
	LastAccessAt int64  // lần truy cập gần nhất (Unix ns); dùng cho eviction LRU (allkeys-lru, volatile-lru)
	AccessCount  uint64 // số lần truy cập; dùng cho eviction LFU (allkeys-lfu)
}

type EvictionPolicy string

const (
	PolicyNoEviction    EvictionPolicy = "noeviction"
	PolicyAllKeysLRU    EvictionPolicy = "allkeys-lru"
	PolicyVolatileLRU   EvictionPolicy = "volatile-lru"
	PolicyAllKeysLFU    EvictionPolicy = "allkeys-lfu"
	PolicyAllKeysRandom EvictionPolicy = "allkeys-random"
)

type Config struct {
	MaxMemory      int64
	EvictionPolicy EvictionPolicy
}

// Metrics contains current resource and key-expiration measurements for a Store.
type Metrics struct {
	KeyCount         int
	MemoryUsageBytes int64
	ExpiringKeys     int
}

// State contains the current distribution of values by Redis data type.
type State struct {
	StringKeys int
	ListKeys   int
	SetKeys    int
	HashKeys   int
}

// Info is a point-in-time Store snapshot intended for diagnostics and monitoring.
type Info struct {
	Config  Config
	Metrics Metrics
	State   State
}

type Store struct {
	mu     sync.RWMutex
	data   map[string]*Value // key -> Value
	config Config
	rand   *rand.Rand
}

func NewStore() *Store {
	return NewStoreWithConfig(Config{})
}

func NewStoreWithConfig(config Config) *Store {
	if config.EvictionPolicy == "" {
		config.EvictionPolicy = PolicyNoEviction
	}
	return &Store{
		data:   make(map[string]*Value),
		config: config,
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func ParseEvictionPolicy(policy string) EvictionPolicy {
	switch EvictionPolicy(strings.ToLower(policy)) {
	case PolicyAllKeysLRU, PolicyVolatileLRU, PolicyAllKeysLFU, PolicyAllKeysRandom, PolicyNoEviction:
		return EvictionPolicy(strings.ToLower(policy))
	default:
		return PolicyNoEviction
	}
}

func (s *Store) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// Metrics returns current memory, key-count, and expiration measurements.
func (s *Store) Metrics() Metrics {
	return s.Info().Metrics
}

// State returns the current number of keys for each supported data type.
func (s *Store) State() State {
	return s.Info().State
}

// Info returns a consistent diagnostics snapshot of the Store.
func (s *Store) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deleteExpiredKeysLocked(time.Now().UnixMilli())
	info := Info{
		Config: s.config,
		Metrics: Metrics{
			KeyCount:         len(s.data),
			MemoryUsageBytes: s.memoryUsageLocked(),
		},
	}
	for _, value := range s.data {
		if value.ExpiresAt > 0 {
			info.Metrics.ExpiringKeys++
		}
		switch value.Type {
		case StringType:
			info.State.StringKeys++
		case ListType:
			info.State.ListKeys++
		case SetType:
			info.State.SetKeys++
		case HashType:
			info.State.HashKeys++
		}
	}
	return info
}

func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpiredKeysLocked(time.Now().UnixMilli())
	return len(s.data)
}

func (s *Store) MemoryUsage() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpiredKeysLocked(time.Now().UnixMilli())
	return s.memoryUsageLocked()
}

func (s *Store) Set(key string, val []byte, ttlMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeLocked(key, func(now int64) error {
		v := &Value{Type: StringType, Str: append([]byte(nil), val...), CreatedAt: now}
		if ttlMs > 0 {
			v.ExpiresAt = now + ttlMs
		}
		s.touchValueLocked(v, now)
		s.data[key] = v
		fmt.Printf("[STORE] SET key=%q estimated=%dB used_memory=%dB max_memory=%dB\n",
			key,
			int64(len(key))+estimateValueSize(v),
			s.memoryUsageLocked(),
			s.config.MaxMemory,
		)
		return nil
	})
}

func (s *Store) SetAt(key string, val []byte, expiresAtMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeLocked(key, func(now int64) error {
		v := &Value{Type: StringType, Str: append([]byte(nil), val...), CreatedAt: now}
		if expiresAtMs > 0 {
			v.ExpiresAt = expiresAtMs
		}
		s.touchValueLocked(v, now)
		s.data[key] = v
		return nil
	})
}

func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok {
		return nil, false
	}

	if v.ExpiresAt > 0 && time.Now().UnixMilli() >= v.ExpiresAt {
		delete(s.data, key)
		return nil, false
	}

	if v.Type != StringType {
		return nil, false
	}
	s.touchValueLocked(v, time.Now().UnixMilli())
	return append([]byte(nil), v.Str...), true
}

func (s *Store) Del(keys []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	now := time.Now().UnixMilli()
	for _, key := range keys {
		if s.deleteExpiredLocked(key, now) {
			continue
		}
		if _, ok := s.data[key]; ok {
			delete(s.data, key)
			deleted++
		}
	}
	return deleted
}

func (s *Store) writeLocked(key string, mutate func(now int64) error) error {
	oldValue, oldExists := s.data[key]
	oldClone := cloneValue(oldValue)
	now := time.Now().UnixMilli()

	if err := mutate(now); err != nil {
		return err
	}
	if err := s.enforceMemoryLimitLocked(now, key); err != nil {
		// roolback
		if oldExists {
			s.data[key] = oldClone
		} else {
			delete(s.data, key)
		}
		return err
	}
	return nil
}

func (s *Store) enforceMemoryLimitLocked(now int64, protectedKey string) error {
	if s.config.MaxMemory <= 0 {
		return nil
	}

	s.deleteExpiredKeysLocked(now)
	for s.memoryUsageLocked() > s.config.MaxMemory {
		key, ok := s.pickEvictionCandidateLocked(protectedKey)
		if !ok {
			return ErrMaxMemory
		}
		usedBefore := s.memoryUsageLocked()
		freed := int64(len(key)) + estimateValueSize(s.data[key])
		delete(s.data, key)
		fmt.Printf("[STORE] EVICT key=%q freed=%dB used_memory=%dB -> %dB limit=%dB\n",
			key,
			freed,
			usedBefore,
			s.memoryUsageLocked(),
			s.config.MaxMemory,
		)
	}
	return nil
}

func (s *Store) pickEvictionCandidateLocked(protectedKey string) (string, bool) {
	switch s.config.EvictionPolicy {
	case PolicyAllKeysLRU:
		return s.pickLRULocked(false, protectedKey)
	case PolicyVolatileLRU:
		return s.pickLRULocked(true, protectedKey)
	case PolicyAllKeysLFU:
		return s.pickLFULocked(protectedKey)
	case PolicyAllKeysRandom:
		return s.pickRandomLocked(protectedKey)
	default:
		return "", false
	}
}

func (s *Store) pickLRULocked(volatileOnly bool, protectedKey string) (string, bool) {
	var selected string
	var selectedAt int64
	found := false
	for key, val := range s.data {
		if key == protectedKey {
			continue
		}
		if volatileOnly && val.ExpiresAt == 0 {
			continue
		}
		if !found || val.LastAccessAt < selectedAt {
			selected = key
			selectedAt = val.LastAccessAt
			found = true
		}
	}
	return selected, found
}

func (s *Store) pickLFULocked(protectedKey string) (string, bool) {
	var selected string
	var selectedCount uint64
	var selectedAt int64
	found := false
	for key, val := range s.data {
		if key == protectedKey {
			continue
		}
		if !found || val.AccessCount < selectedCount || (val.AccessCount == selectedCount && val.LastAccessAt < selectedAt) {
			selected = key
			selectedCount = val.AccessCount
			selectedAt = val.LastAccessAt
			found = true
		}
	}
	return selected, found
}

func (s *Store) pickRandomLocked(protectedKey string) (string, bool) {
	candidates := make([]string, 0, len(s.data))
	for key := range s.data {
		if key != protectedKey {
			candidates = append(candidates, key)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	return candidates[s.rand.Intn(len(candidates))], true
}

func (s *Store) memoryUsageLocked() int64 {
	var total int64
	for key, val := range s.data {
		total += int64(len(key)) + estimateValueSize(val)
	}
	return total
}

func estimateValueSize(v *Value) int64 {
	if v == nil {
		return 0
	}

	// 64: Approximate metadata cost per stored value for memory-limit eviction.
	const valueOverhead = int64(64)
	total := valueOverhead
	switch v.Type {
	case StringType:
		total += int64(len(v.Str))
	case ListType:
		for _, item := range v.List {
			total += int64(len(item)) + 8
		}
	case SetType:
		for member := range v.Set {
			total += int64(len(member)) + 16
		}
	case HashType:
		for field, val := range v.Hash {
			total += int64(len(field)) + int64(len(val)) + 16
		}
	}
	return total
}

func cloneValue(v *Value) *Value {
	if v == nil {
		return nil
	}
	clone := *v
	clone.Str = append([]byte(nil), v.Str...)
	if v.List != nil {
		clone.List = make([][]byte, len(v.List))
		for i, item := range v.List {
			clone.List[i] = append([]byte(nil), item...)
		}
	}
	if v.Set != nil {
		clone.Set = make(map[string]struct{}, len(v.Set))
		for member := range v.Set {
			clone.Set[member] = struct{}{}
		}
	}
	if v.Hash != nil {
		clone.Hash = make(map[string][]byte, len(v.Hash))
		for field, val := range v.Hash {
			clone.Hash[field] = append([]byte(nil), val...)
		}
	}
	return &clone
}

func (s *Store) touchValueLocked(v *Value, now int64) {
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.LastAccessAt = now
	v.AccessCount++
}

func (s *Store) LPush(key string, vals ...[]byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	newLen := 0
	err := s.writeLocked(key, func(now int64) error {
		v, ok := s.data[key]
		if ok && s.valueExpiredLocked(key, v, now) {
			ok = false
		}
		if !ok {
			v = &Value{Type: ListType, CreatedAt: now}
			s.data[key] = v
		}
		if v.Type != ListType {
			return ErrWrongType
		}

		newItems := make([][]byte, len(vals))
		for i, val := range vals {
			newItems[i] = append([]byte(nil), val...)
		}
		for i, j := 0, len(newItems)-1; i < j; i, j = i+1, j-1 {
			newItems[i], newItems[j] = newItems[j], newItems[i]
		}
		v.List = append(newItems, v.List...)
		s.touchValueLocked(v, now)
		newLen = len(v.List)
		return nil
	})
	return newLen, err
}

func (s *Store) RPush(key string, vals ...[]byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	newLen := 0
	err := s.writeLocked(key, func(now int64) error {
		v, ok := s.data[key]
		if ok && s.valueExpiredLocked(key, v, now) {
			ok = false
		}
		if !ok {
			v = &Value{Type: ListType, CreatedAt: now}
			s.data[key] = v
		}
		if v.Type != ListType {
			return ErrWrongType
		}

		for _, val := range vals {
			v.List = append(v.List, append([]byte(nil), val...))
		}
		s.touchValueLocked(v, now)
		newLen = len(v.List)
		return nil
	})
	return newLen, err
}

func (s *Store) LRange(key string, start, stop int) ([][]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return nil, true
	}
	if v.Type != ListType {
		return nil, false
	}
	s.touchValueLocked(v, time.Now().UnixMilli())

	l := len(v.List)
	if l == 0 {
		return nil, true
	}
	if start < 0 {
		start = l + start
	}
	if stop < 0 {
		stop = l + stop
	}
	if start < 0 {
		start = 0
	}
	if stop >= l {
		stop = l - 1
	}
	if start >= l || start > stop {
		return nil, true
	}

	out := make([][]byte, 0, stop-start+1)
	for _, item := range v.List[start : stop+1] {
		out = append(out, append([]byte(nil), item...))
	}
	return out, true
}

func (s *Store) SAdd(key string, val []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	added := false
	err := s.writeLocked(key, func(now int64) error {
		v, ok := s.data[key]
		if ok && s.valueExpiredLocked(key, v, now) {
			ok = false
		}
		if !ok {
			v = &Value{Type: SetType, Set: make(map[string]struct{}), CreatedAt: now}
			s.data[key] = v
		}
		if v.Type != SetType {
			return ErrWrongType
		}

		_, exists := v.Set[string(val)]
		v.Set[string(val)] = struct{}{}
		s.touchValueLocked(v, now)
		added = !exists
		return nil
	})
	return added, err
}

func (s *Store) SMembers(key string) ([][]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return nil, true
	}
	if v.Type != SetType {
		return nil, false
	}
	s.touchValueLocked(v, time.Now().UnixMilli())

	members := make([][]byte, 0, len(v.Set))
	for k := range v.Set {
		members = append(members, []byte(k))
	}
	return members, true
}

func (s *Store) HSet(key, field string, val []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	created := 0
	err := s.writeLocked(key, func(now int64) error {
		v, ok := s.data[key]
		if ok && s.valueExpiredLocked(key, v, now) {
			ok = false
		}
		if !ok {
			v = &Value{Type: HashType, Hash: make(map[string][]byte), CreatedAt: now}
			s.data[key] = v
		}
		if v.Type != HashType {
			return ErrWrongType
		}

		_, exists := v.Hash[field]
		v.Hash[field] = append([]byte(nil), val...)
		s.touchValueLocked(v, now)

		if !exists {
			created = 1
		}
		return nil
	})
	return created, err
}

func (s *Store) HGet(key, field string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) || v.Type != HashType {
		return nil, false
	}

	val, exists := v.Hash[field]
	if !exists {
		return nil, false
	}
	s.touchValueLocked(v, time.Now().UnixMilli())
	return append([]byte(nil), val...), true
}

func (s *Store) valueExpiredLocked(key string, v *Value, now int64) bool {
	if v.ExpiresAt == 0 {
		return false
	}
	if now >= v.ExpiresAt {
		delete(s.data, key)
		return true
	}
	return false
}

func (s *Store) deleteExpiredKeysLocked(now int64) {
	for key, val := range s.data {
		if val.ExpiresAt > 0 && now >= val.ExpiresAt {
			delete(s.data, key)
		}
	}
}

func (s *Store) deleteExpiredLocked(key string, now int64) bool {
	v, ok := s.data[key]
	if !ok {
		return false
	}
	return s.valueExpiredLocked(key, v, now)
}

func (s *Store) Expire(key string, seconds int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return false
	}
	now := time.Now().UnixMilli()
	v.ExpiresAt = now + seconds*1000
	s.touchValueLocked(v, now)
	return true
}

func (s *Store) ExpireAt(key string, expiresAtMs int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return false
	}
	v.ExpiresAt = expiresAtMs
	s.touchValueLocked(v, time.Now().UnixMilli())
	return true
}

func (s *Store) TTL(key string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return -2
	}
	if s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return -2
	}
	if v.ExpiresAt == 0 {
		return -1
	}
	ttl := v.ExpiresAt - time.Now().UnixMilli()
	if ttl <= 0 {
		delete(s.data, key)
		return -2
	}
	return ttl / 1000
}

// StartTTLCheckerContext removes expired keys until ctx is cancelled.
func (s *Store) StartTTLCheckerContext(ctx context.Context, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now().UnixMilli()
				s.mu.Lock()
				for key, val := range s.data {
					if val.ExpiresAt > 0 && now >= val.ExpiresAt {
						delete(s.data, key)
					}
				}
				s.mu.Unlock()
			}
		}
	}()
	return done
}

// StartEvictionCheckerContext enforces the memory limit until ctx is cancelled.
func (s *Store) StartEvictionCheckerContext(ctx context.Context, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now().UnixMilli()
				s.mu.Lock()
				_ = s.enforceMemoryLimitLocked(now, "")
				s.mu.Unlock()
			}
		}
	}()
	return done
}
