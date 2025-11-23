package command

import (
	"redis-clone/internal/auth"
	"redis-clone/internal/store"
)

type CommandContext struct {
	Writer        *RespWriter
	Store         *store.Store
	Accounts      *auth.AccountStore
	Authenticated bool
	Username      string
}
