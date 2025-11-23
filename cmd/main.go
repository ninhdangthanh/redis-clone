package main

import (
	"bufio"
	"fmt"
	"net"
	"redis-clone/internal"
	"redis-clone/internal/auth"
	"redis-clone/internal/command"
	"redis-clone/internal/dispatcher"
	"redis-clone/internal/store"
	"strconv"
	"strings"
	"time"
)

func handleConn(conn net.Conn, store *store.Store, accounts *auth.AccountStore) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	commandContext := &command.CommandContext{
		Writer:        command.NewRespWriter(w),
		Store:         store,
		Accounts:      accounts,
		Authenticated: false,
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

		dispatcher.Dispatch(commandContext, args)

		w.Flush()
	}
}

func main() {
	ln, err := net.Listen("tcp", ":6379")
	if err != nil {
		panic(err)
	}
	fmt.Println("listening on :6379")
	store := store.NewStore()
	accounts := auth.NewAccountStore()

	accounts.AddUser("admin", "admin")
	accounts.AddUser("ninh", "ninh")
	accounts.AddUser("dev", "dev")
	accounts.AddUser("ba", "ba")

	store.StartTTLChecker(time.Second)

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleConn(conn, store, accounts)
	}
}
