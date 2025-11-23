package main

import (
	"bufio"
	"fmt"
	"net"
	"redis-clone/internal"
	"strconv"
	"strings"
	"time"
)

func handleConn(conn net.Conn, store *internal.Store, accounts *internal.AccountStore) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	var authenticated bool
	var username string

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

		// --- basic commands ---
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
					username = user
					fmt.Fprint(w, "+OK\r\n")
				} else {
					fmt.Fprint(w, "-ERR invalid username/password\r\n")
				}
			}

		case "QUIT":
			fmt.Fprint(w, "+OK\r\n")
			w.Flush()
			return

		// --- string commands ---
		case "SET":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}

			if len(args) != 3 && len(args) != 5 {
				fmt.Fprint(w, "-ERR wrong number of arguments for SET\r\n")
				break
			}

			key := args[1]
			val := []byte(args[2])
			var ttlMs int64 = 0

			if len(args) >= 5 {
				switch strings.ToUpper(args[3]) {
				case "EX":
					seconds, err := strconv.ParseInt(args[4], 10, 64)
					if err != nil || seconds < 0 {
						fmt.Fprint(w, "-ERR invalid expire time\r\n")
						break
					}
					ttlMs = seconds * 1000
				case "PX":
					ms, err := strconv.ParseInt(args[4], 10, 64)
					if err != nil || ms < 0 {
						fmt.Fprint(w, "-ERR invalid expire time\r\n")
						break
					}
					ttlMs = ms
				default:
					fmt.Fprint(w, "-ERR syntax error\r\n")
					continue
				}
			}

			// Pass ttlMs = 0 for no expiration
			store.Set(username, key, val, ttlMs)
			fmt.Fprint(w, "+OK\r\n")

		case "GET":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}
			if len(args) != 2 {
				fmt.Fprint(w, "-ERR wrong number of arguments for GET\r\n")
			} else {
				val, ok := store.Get(username, args[1])
				if !ok {
					fmt.Fprint(w, "$-1\r\n")
				} else {
					fmt.Fprintf(w, "$%d\r\n%s\r\n", len(val), val)
				}
			}

		// --- list commands ---
		case "LPUSH":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}
			if len(args) < 3 {
				fmt.Fprint(w, "-ERR wrong number of arguments for LPUSH\r\n")
				break
			}
			for _, val := range args[2:] {
				store.LPush(username, args[1], []byte(val))
			}
			fmt.Fprintf(w, ":%d\r\n", len(store.LRange(username, args[1], 0, -1)))

		case "RPUSH":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}
			if len(args) < 3 {
				fmt.Fprint(w, "-ERR wrong number of arguments for RPUSH\r\n")
				break
			}
			for _, val := range args[2:] {
				store.RPush(username, args[1], []byte(val))
			}
			fmt.Fprintf(w, ":%d\r\n", len(store.LRange(username, args[1], 0, -1)))

		case "LRANGE":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}
			if len(args) != 4 {
				fmt.Fprint(w, "-ERR wrong number of arguments for LRANGE\r\n")
				break
			}
			start, _ := strconv.Atoi(args[2])
			stop, _ := strconv.Atoi(args[3])
			values := store.LRange(username, args[1], start, stop)
			fmt.Fprintf(w, "*%d\r\n", len(values))
			for _, val := range values {
				fmt.Fprintf(w, "$%d\r\n%s\r\n", len(val), val)
			}

		// --- set commands ---
		case "SADD":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}
			if len(args) < 3 {
				fmt.Fprint(w, "-ERR wrong number of arguments for SADD\r\n")
				break
			}
			added := 0
			for _, val := range args[2:] {
				if store.SAdd(username, args[1], []byte(val)) {
					added++
				}
			}
			fmt.Fprintf(w, ":%d\r\n", added)

		case "SMEMBERS":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}
			if len(args) != 2 {
				fmt.Fprint(w, "-ERR wrong number of arguments for SMEMBERS\r\n")
				break
			}
			members := store.SMembers(username, args[1])
			fmt.Fprintf(w, "*%d\r\n", len(members))
			for _, val := range members {
				fmt.Fprintf(w, "$%d\r\n%s\r\n", len(val), val)
			}

		// --- hash commands ---
		case "HSET":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}
			if len(args) != 4 {
				fmt.Fprint(w, "-ERR wrong number of arguments for HSET\r\n")
				break
			}
			newField := store.HSet(username, args[1], args[2], []byte(args[3]))
			fmt.Fprintf(w, ":%d\r\n", newField)

		case "HGET":
			if !authenticated {
				fmt.Fprint(w, "-NOAUTH Authentication required\r\n")
				break
			}
			if len(args) != 3 {
				fmt.Fprint(w, "-ERR wrong number of arguments for HGET\r\n")
				break
			}
			val, ok := store.HGet(username, args[1], args[2])
			if !ok {
				fmt.Fprint(w, "$-1\r\n")
			} else {
				fmt.Fprintf(w, "$%d\r\n%s\r\n", len(val), val)
			}

		case "EXPIRE":
			if len(args) != 3 {
				fmt.Fprint(w, "-ERR wrong number of arguments for EXPIRE\r\n")
				break
			}
			seconds, _ := strconv.ParseInt(args[2], 10, 64)
			if store.Expire(username, args[1], seconds) {
				fmt.Fprint(w, ":1\r\n")
			} else {
				fmt.Fprint(w, ":0\r\n")
			}

		case "TTL":
			if len(args) != 2 {
				fmt.Fprint(w, "-ERR wrong number of arguments for TTL\r\n")
				break
			}
			fmt.Fprintf(w, ":%d\r\n", store.TTL(username, args[1]))

		// --- unknown command ---
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
