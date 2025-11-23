package internal

import "sync"

type AccountStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewAccountStore() *AccountStore {
	return &AccountStore{data: make(map[string]string)}
}

func (a *AccountStore) AddUser(user, pass string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data[user] = pass
}

func (a *AccountStore) Validate(user, pass string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p, ok := a.data[user]
	return ok && p == pass
}
