package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"redis-clone/internal"
	"redis-clone/internal/aof"
	"redis-clone/internal/command"
	"redis-clone/internal/dispatcher"
	"redis-clone/internal/store"
	"strconv"
	"strings"
	"time"
)

type AOFCommandContext struct {
	*command.CommandContext
	Success bool
}

func handleConn(conn net.Conn, store *store.Store, aof *aof.AOF) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	commandContext := &AOFCommandContext{
		CommandContext: &command.CommandContext{
			Writer:        command.NewRespWriter(w),
			Store:         store,
			Authenticated: true, // TODO: this is temporary, implement AUTH later
		},
		Success: false,
	}

	for {
		line, err := internal.ReadLine(r)
		if err != nil {
			return
		}
		if !strings.HasPrefix(line, "*") {
			fmt.Fprint(w, "-ERR only RESP arrays supported\r\n")
			w.Flush()
			continue
		}

		n, _ := strconv.Atoi(line[1:])
		args := make([]string, 0, n)
		for i := 0; i < n; i++ {
			s, err := internal.ReadBulkString(r)
			if err != nil {
				return
			}
			args = append(args, s)
		}

		if len(args) == 0 {
			continue
		}

		cmd := strings.ToUpper(args[0])

		commandContext.Success = dispatcher.Dispatch(commandContext.CommandContext, args)

		if commandContext.Success && aof.ShouldPersistCommand(cmd) {
			if err := aof.Append(args); err != nil {
				fmt.Printf("AOF append error: %v\n", err)
			}
		}

		w.Flush()

		if cmd == "QUIT" && commandContext.Success {
			return
		}
	}
}

func main() {
	aof, err := aof.Open("appendonly.aof", aof.FsyncEverySec)
	if err != nil {
		panic(fmt.Sprintf("Failed to open AOF: %v", err))
	}
	defer func() {
		if err := aof.Close(); err != nil {
			fmt.Printf("Error closing AOF: %v\n", err)
		}
	}()

	store := store.NewStore()

	store.StartTTLChecker(time.Second)

	fmt.Println("Replaying AOF to restore state...")
	if err := aof.Replay(func(args []string) error {
		if len(args) == 0 {
			return nil
		}

		cmd := strings.ToUpper(args[0])
		if !aof.ShouldPersistCommand(cmd) {
			return nil
		}

		tempCtx := &command.CommandContext{
			Writer:        command.NewRespWriter(bufio.NewWriter(io.Discard)),
			Store:         store,
			Authenticated: true,
		}

		success := dispatcher.Dispatch(tempCtx, args)
		if !success {
			return fmt.Errorf("failed to replay command: %v", args)
		}

		fmt.Printf("Replayed command: %s\n", cmd)
		return nil
	}); err != nil {
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
		go handleConn(conn, store, aof)
	}
}
