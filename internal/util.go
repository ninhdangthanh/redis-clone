package internal

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RESP helpers
func ReadLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func ReadBulkString(r *bufio.Reader) (string, error) {
	lenLine, err := ReadLine(r)
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
