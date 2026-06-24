package main

import (
	"bufio"
	"context"
	"fmt"
	"redis-clone/internal/aof"
	"redis-clone/internal/command"
	"redis-clone/internal/dispatcher"
	"redis-clone/internal/pubsub"
	"redis-clone/internal/store"
	"strings"
	"sync"
	"time"
)

// CommandExecutor is the server's single command event loop. Connection
// goroutines may read client sockets concurrently, but they must submit normal
// commands here and wait for their result. This gives normal client commands a
// single, deterministic execution order. The Store mutex remains necessary for
// background TTL and eviction workers.
type CommandExecutor struct {
	ctx      context.Context
	store    *store.Store
	aof      *aof.AOF
	pubsub   *pubsub.Hub
	requests chan commandRequest
	done     chan struct{}
	stopOnce sync.Once
}

type commandRequest struct {
	writer *bufio.Writer
	args   []string
	done   chan commandResult
}

type commandResult struct {
	quit bool
}

func NewCommandExecutor(ctx context.Context, st *store.Store, aofFile *aof.AOF, hub *pubsub.Hub) *CommandExecutor {
	e := &CommandExecutor{
		ctx:    ctx,
		store:  st,
		aof:    aofFile,
		pubsub: hub,
		// A bounded queue applies TCP backpressure when command execution cannot
		// keep up, instead of letting pending commands grow without limit.
		requests: make(chan commandRequest, 256),
		done:     make(chan struct{}),
	}
	go e.run()
	return e
}

func (e *CommandExecutor) run() {
	defer close(e.done)
	for {
		select {
		case <-e.ctx.Done():
			return
		case request := <-e.requests:
			result := e.execute(request.writer, request.args)
			select {
			case request.done <- result:
			case <-e.ctx.Done():
				return
			}
		}
	}
}

func (e *CommandExecutor) execute(w *bufio.Writer, args []string) commandResult {
	ctx := &AOFCommandContext{
		CommandContext: &command.CommandContext{
			Writer:        command.NewRespWriter(w),
			Store:         e.store,
			PubSub:        e.pubsub,
			Authenticated: true, // TODO: implement AUTH
		},
	}

	cmd := strings.ToUpper(args[0])
	ctx.Success = dispatcher.Dispatch(ctx.CommandContext, args)
	if ctx.Success && e.aof.ShouldPersistCommand(cmd) {
		if err := e.aof.Append(aof.ArgsForAppend(args, time.Now())); err != nil {
			// The response has already been written. Keep this behavior consistent
			// with the previous connection-local dispatcher.
			fmt.Printf("AOF append error: %v\n", err)
		}
	}
	_ = w.Flush()
	return commandResult{quit: cmd == "QUIT" && ctx.Success}
}

// Submit blocks the caller until its command has been executed by the event
// loop, which preserves Redis response ordering for pipelined requests on one
// connection. It returns false if server shutdown began before completion.
func (e *CommandExecutor) Submit(ctx context.Context, w *bufio.Writer, args []string) (result commandResult, ok bool) {
	request := commandRequest{writer: w, args: args, done: make(chan commandResult, 1)}
	select {
	case e.requests <- request:
	case <-ctx.Done():
		return commandResult{}, false
	case <-e.done:
		return commandResult{}, false
	}

	select {
	case result = <-request.done:
		return result, true
	case <-ctx.Done():
		return commandResult{}, false
	case <-e.done:
		return commandResult{}, false
	}
}

func (e *CommandExecutor) Stop(ctx context.Context) error {
	e.stopOnce.Do(func() {})
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
