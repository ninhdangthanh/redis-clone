package aof

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type FsyncMode int

const (
	FsyncAlways FsyncMode = iota
	FsyncEverySec
	FsyncNever
)

type AOF struct {
	mu     sync.Mutex
	f      *os.File
	w      *bufio.Writer
	mode   FsyncMode
	quitCh chan struct{}
}

func ParseMode(s string) FsyncMode {
	switch strings.ToLower(s) {
	case "always":
		return FsyncAlways
	case "never":
		return FsyncNever
	default:
		return FsyncEverySec
	}
}

func Open(path string, mode FsyncMode) (*AOF, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	a := &AOF{
		f:      f,
		w:      bufio.NewWriter(f),
		mode:   mode,
		quitCh: make(chan struct{}),
	}

	if mode == FsyncEverySec {
		go a.syncEverySecond()
	}

	return a, nil
}

func (a *AOF) syncEverySecond() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.mu.Lock()
			if err := a.w.Flush(); err != nil {
				fmt.Printf("AOF flush error: %v\n", err)
			}
			if err := a.f.Sync(); err != nil {
				fmt.Printf("AOF sync error: %v\n", err)
			}
			a.mu.Unlock()
		case <-a.quitCh:
			return
		}
	}
}

func (a *AOF) Close() error {
	close(a.quitCh)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.w != nil {
		a.w.Flush()
	}
	if a.f != nil {
		return a.f.Close()
	}
	return nil
}

// Append writes the command as a RESP array to the AOF file.
func (a *AOF) Append(args []string) error {
	if a == nil {
		return errors.New("aof is nil")
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// encode as RESP array
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
	}

	if _, err := a.w.WriteString(b.String()); err != nil {
		return err
	}

	if a.mode == FsyncAlways {
		if err := a.w.Flush(); err != nil {
			return err
		}
		return a.f.Sync()
	}
	if a.mode == FsyncNever {
		return a.w.Flush()
	}
	return nil
}

// Replay reads the AOF file and yields parsed commands one by one via the provided callback.
// The callback receives a slice of arguments for each command and should apply them to state.
func (a *AOF) Replay(cb func(args []string) error) error {
	if a == nil || a.f == nil {
		return errors.New("aof not opened")
	}
	// Seek to beginning and read
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.f.Seek(0, io.SeekStart); err != nil {
		return err
	}

	r := bufio.NewReader(a.f)
	for {
		// Read array header
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		if line[0] != '*' {
			return fmt.Errorf("invalid aof format: expected array header, got %q", line)
		}
		var n int
		_, err = fmt.Sscanf(line, "*%d", &n)
		if err != nil {
			return err
		}

		args := make([]string, 0, n)
		for i := 0; i < n; i++ {
			// read bulk header
			bl, err := r.ReadString('\n')
			if err != nil {
				return err
			}
			bl = strings.TrimRight(bl, "\r\n")
			if bl == "" || bl[0] != '$' {
				return fmt.Errorf("invalid bulk header: %q", bl)
			}
			var l int
			_, err = fmt.Sscanf(bl, "$%d", &l)
			if err != nil {
				return err
			}
			// read payload of length l + CRLF
			buf := make([]byte, l+2)
			if _, err := io.ReadFull(r, buf); err != nil {
				return err
			}
			// trim CRLF
			arg := string(buf[:l])
			args = append(args, arg)
		}

		if err := cb(args); err != nil {
			return err
		}
	}
}

func (a *AOF) ShouldPersistCommand(cmd string) bool {
	writeCommands := map[string]bool{
		"SET":    true,
		"DEL":    true,
		"LPUSH":  true,
		"RPUSH":  true,
		"SADD":   true,
		"HSET":   true,
		"EXPIRE": true,
	}
	return writeCommands[cmd]
}
