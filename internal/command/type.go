package command

import (
	"redis-clone/internal/store"
)

type CommandContext struct {
	Writer        *RespWriter
	Store         *store.Store
	Authenticated bool
}
