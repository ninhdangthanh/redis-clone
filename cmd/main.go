package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

type Store struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewStore() *Store {
	return &Store{data: make(map[string][]byte)}
}

func (s *Store) Set(key string, val []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = val
}

func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

// RESP helpers
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func readBulkString(r *bufio.Reader) (string, error) {
	lenLine, err := readLine(r)
	if err != nil {
		return "", err
	}
	if lenLine == "$-1" {
		return "", nil
	}
	if !strings.HasPrefix(lenLine, "$") {
		return "", fmt.Errorf("expected $, got %q", lenLine)
	}

	l, err := strconv.Atoi(lenLine[1:])
	if err != nil {
		return "", err
	}

	buf := make([]byte, l+2) // include \r\n
	_, err = io.ReadFull(r, buf)
	if err != nil {
		return "", err
	}

	return string(buf[:l]), nil
}

func handleConn(conn net.Conn, store *Store) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	for {
		line, err := readLine(r)
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
			s, err := readBulkString(r)
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

		case "SET":
			if len(args) != 3 {
				fmt.Fprint(w, "-ERR wrong number of arguments for SET\r\n")
			} else {
				store.Set(args[1], []byte(args[2]))
				fmt.Fprint(w, "+OK\r\n")
			}

		case "GET":
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
	store := NewStore()

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleConn(conn, store)
	}
}
