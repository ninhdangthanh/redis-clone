package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"redis-clone/internal"
	"redis-clone/internal/aof"
	"redis-clone/internal/command"
	"redis-clone/internal/dispatcher"
	"redis-clone/internal/pubsub"
	"redis-clone/internal/store"
	"strconv"
	"strings"
	"time"
)

type AOFCommandContext struct {
	*command.CommandContext
	Success bool
}

type parsedCommand struct {
	args        []string
	protocolErr string
	err         error
}

func readRESPCommand(r *bufio.Reader) parsedCommand {
	line, err := internal.ReadLine(r)
	if err != nil {
		return parsedCommand{err: err}
	}
	if !strings.HasPrefix(line, "*") {
		return parsedCommand{protocolErr: "ERR only RESP arrays supported"}
	}

	n, err := strconv.Atoi(line[1:])
	if err != nil || n <= 0 {
		return parsedCommand{protocolErr: "ERR invalid RESP array length"}
	}

	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		s, err := internal.ReadBulkString(r)
		if err != nil {
			return parsedCommand{err: err}
		}
		args = append(args, s)
	}
	return parsedCommand{args: args}
}

func readCommands(r *bufio.Reader) <-chan parsedCommand {
	ch := make(chan parsedCommand, 1)
	go func() {
		defer close(ch)
		for {
			parsed := readRESPCommand(r)
			ch <- parsed
			if parsed.err != nil {
				return
			}
		}
	}()
	return ch
}

func handleConn(conn net.Conn, store *store.Store, aofFile *aof.AOF, pubsubHub *pubsub.Hub) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	cmdCh := readCommands(r)

	commandContext := &AOFCommandContext{
		CommandContext: &command.CommandContext{
			Writer:        command.NewRespWriter(w),
			Store:         store,
			PubSub:        pubsubHub,
			Authenticated: true, // TODO: this is temporary, implement AUTH later
		},
		Success: false,
	}

	for {
		parsed, ok := <-cmdCh
		if !ok || parsed.err != nil {
			return
		}
		if parsed.protocolErr != "" {
			fmt.Fprintf(w, "-%s\r\n", parsed.protocolErr)
			w.Flush()
			continue
		}

		args := parsed.args
		if len(args) == 0 {
			continue
		}

		cmd := strings.ToUpper(args[0])
		if cmd == "SUBSCRIBE" {
			if len(args) < 2 {
				commandContext.Writer.WriteError("wrong number of arguments for SUBSCRIBE")
				continue
			}
			if serveSubscribedConn(cmdCh, commandContext.CommandContext, args) {
				return
			}
			continue
		}

		commandContext.Success = dispatcher.Dispatch(commandContext.CommandContext, args)

		if commandContext.Success && aofFile.ShouldPersistCommand(cmd) {
			if err := aofFile.Append(aof.ArgsForAppend(args, time.Now())); err != nil {
				fmt.Printf("AOF append error: %v\n", err)
			}
		}

		w.Flush()

		if cmd == "QUIT" && commandContext.Success {
			return
		}
	}
}

func serveSubscribedConn(cmdCh <-chan parsedCommand, ctx *command.CommandContext, initialArgs []string) bool {
	sub := ctx.PubSub.NewSubscriber()
	defer ctx.PubSub.UnsubscribeAll(sub)

	subscribeChannels(ctx, sub, initialArgs[1:])

	for ctx.PubSub.SubscriptionCount(sub) > 0 {
		select {
		case parsed, ok := <-cmdCh:
			if !ok || parsed.err != nil {
				return true
			}
			if parsed.protocolErr != "" {
				ctx.Writer.WriteError(parsed.protocolErr)
				continue
			}
			if handleSubscribedModeCommand(ctx, sub, parsed.args) {
				return true
			}
		case msg := <-sub.Messages:
			command.WritePubSubMessage(ctx.Writer, msg)
		}
	}

	return false
}

func subscribeChannels(ctx *command.CommandContext, sub *pubsub.Subscriber, channels []string) {
	for _, channel := range channels {
		count := ctx.PubSub.Subscribe(sub, channel)
		command.WriteSubscribeReply(ctx.Writer, "subscribe", channel, count)
	}
}

func handleSubscribedModeCommand(ctx *command.CommandContext, sub *pubsub.Subscriber, args []string) bool {
	if len(args) == 0 {
		return false
	}

	cmd := strings.ToUpper(args[0])
	switch cmd {
	case "SUBSCRIBE":
		if len(args) < 2 {
			ctx.Writer.WriteError("wrong number of arguments for SUBSCRIBE")
			return false
		}
		subscribeChannels(ctx, sub, args[1:])
	case "UNSUBSCRIBE":
		unsubscribeChannels(ctx, sub, args[1:])
	case "PING":
		writeSubscribedPong(ctx.Writer, args)
	case "QUIT":
		command.HandleQuit(ctx, args)
		return true
	default:
		ctx.Writer.WriteError("ERR Can't execute command in subscribed mode")
	}
	return false
}

func unsubscribeChannels(ctx *command.CommandContext, sub *pubsub.Subscriber, channels []string) {
	if len(channels) == 0 {
		channels = sub.Channels()
		if len(channels) == 0 {
			command.WriteSubscribeReply(ctx.Writer, "unsubscribe", "", 0)
			return
		}
	}

	for _, channel := range channels {
		count := ctx.PubSub.Unsubscribe(sub, channel)
		command.WriteSubscribeReply(ctx.Writer, "unsubscribe", channel, count)
	}
}

func writeSubscribedPong(w *command.RespWriter, args []string) {
	if len(args) > 2 {
		w.WriteError("wrong number of arguments for PING")
		return
	}
	msg := ""
	if len(args) == 2 {
		msg = args[1]
	}
	w.WriteArrayHeader(2)
	w.WriteBulkString([]byte("pong"))
	w.WriteBulkString([]byte(msg))
}

func ReplayAOF(store *store.Store, aofFile *aof.AOF) error {
	fmt.Println("Replaying AOF to restore state...")

	return aofFile.Replay(func(args []string) error {
		if len(args) == 0 {
			return nil
		}

		cmd := strings.ToUpper(args[0])
		if !aofFile.ShouldPersistCommand(cmd) {
			return nil
		}

		silentWriter := command.NewRespWriter(bufio.NewWriter(io.Discard))

		tempCtx := &command.CommandContext{
			Writer:        silentWriter,
			Store:         store,
			Authenticated: true,
		}

		success := dispatcher.Dispatch(tempCtx, args)
		if !success {
			return fmt.Errorf("failed to replay command: %v", args)
		}

		fmt.Printf("Replayed command: %s\n", cmd)
		return nil
	})
}

func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func storeConfigFromEnv() store.Config {
	config := store.Config{
		EvictionPolicy: store.ParseEvictionPolicy(os.Getenv("MAXMEMORY_POLICY")),
	}
	if raw := os.Getenv("MAXMEMORY"); raw != "" {
		maxMemory, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && maxMemory > 0 {
			config.MaxMemory = maxMemory
		}
	}
	return config
}

func main() {
	if err := loadEnvFile(".env"); err != nil {
		fmt.Printf("Error loading .env: %v\n", err)
	}

	aofFile, err := aof.Open("appendonly.aof", aof.FsyncEverySec)
	if err != nil {
		panic(fmt.Sprintf("Failed to open AOF: %v", err))
	}
	defer func() {
		if err := aofFile.Close(); err != nil {
			fmt.Printf("Error closing AOF: %v\n", err)
		}
	}()

	store := store.NewStoreWithConfig(storeConfigFromEnv())
	store.StartTTLChecker(time.Second)
	pubsubHub := pubsub.NewHub()

	// change memory limitation,...
	store.StartEvictionChecker(time.Second)

	if err := ReplayAOF(store, aofFile); err != nil {
		fmt.Printf("AOF replay error: %v\n", err)
	} else {
		fmt.Println("AOF replay completed successfully")
	}

	ln, err := net.Listen("tcp", ":6379")
	if err != nil {
		panic(fmt.Sprintf("Failed to start server: %v", err))
	}
	defer ln.Close()

	fmt.Println("Redis clone listening on :6379")

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Printf("Connection error: %v\n", err)
			continue
		}
		go handleConn(conn, store, aofFile, pubsubHub)
	}
}
