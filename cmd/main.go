package main

import (
	"bufio"
	"fmt"
	"net"
	"redis-clone/internal"
	"strconv"
	"strings"
)

func handleConn(conn net.Conn, store *internal.Store, accounts *internal.AccountStore) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	var authenticated bool

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

		switch cmd {
		case "PING":
			if len(args) == 2 {
				fmt.Fprintf(w, "+%s\r\n", args[1])
			} else {
				fmt.Fprint(w, "+PONG\r\n")
			}

		case "ECHO":
			if len(args) != 2 {
				fmt.Fprint(w, "-ERR wrong number of arguments for ECHO\r\n")
			} else {
				fmt.Fprintf(w, "$%d\r\n%s\r\n", len(args[1]), args[1])
			}

		case "AUTH":
			if len(args) != 3 {
				fmt.Fprint(w, "-ERR wrong number of arguments for AUTH\r\n")
			} else {
				user := args[1]
				pass := args[2]

				if accounts.Validate(user, pass) {
					authenticated = true
					fmt.Fprint(w, "+OK\r\n")
				} else {
					fmt.Fprint(w, "-ERR invalid username/password\r\n")
				}
			}

		case "SET":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}

			if len(args) != 3 {
				fmt.Fprint(w, "-ERR wrong number of arguments for SET\r\n")
			} else {
				store.Set(args[1], []byte(args[2]))
				fmt.Fprint(w, "+OK\r\n")
			}

		case "GET":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}

			if len(args) != 2 {
				fmt.Fprint(w, "-ERR wrong number of arguments for GET\r\n")
			} else {
				val, ok := store.Get(args[1])
				if !ok {
					fmt.Fprint(w, "$-1\r\n")
				} else {
					fmt.Fprintf(w, "$%d\r\n%s\r\n", len(val), val)
				}
			}

		default:
			fmt.Fprintf(w, "-ERR unknown command '%s'\r\n", cmd)
		}

		w.Flush()
	}
}

func main() {
	ln, err := net.Listen("tcp", ":6379")
	if err != nil {
		panic(err)
	}
	fmt.Println("listening on :6379")
	store := internal.NewStore()
	accounts := internal.NewAccountStore()

	accounts.AddUser("admin", "admin")
	accounts.AddUser("dev", "dev")
	accounts.AddUser("sale", "sale")

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleConn(conn, store, accounts)
	}
}
