package command

import (
	"redis-clone/internal/pubsub"
	"redis-clone/internal/store"
)

type CommandContext struct {
	Writer        *RespWriter
	Store         *store.Store
	PubSub        *pubsub.Hub
	Authenticated bool
}
