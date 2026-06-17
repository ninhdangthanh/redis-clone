package internal

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadLineTrimsCRLF(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("PING\r\n"))

	got, err := ReadLine(r)
	if err != nil {
		t.Fatalf("ReadLine returned error: %v", err)
	}
	if got != "PING" {
		t.Fatalf("ReadLine = %q, want %q", got, "PING")
	}
}

func TestReadBulkString(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$5\r\nhello\r\n"))

	got, err := ReadBulkString(r)
	if err != nil {
		t.Fatalf("ReadBulkString returned error: %v", err)
	}
	if got != "hello" {
		t.Fatalf("ReadBulkString = %q, want %q", got, "hello")
	}
}

func TestReadBulkStringRejectsInvalidHeader(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("*1\r\n"))

	if _, err := ReadBulkString(r); err == nil {
		t.Fatal("ReadBulkString should reject non-bulk headers")
	}
}

func TestReadBulkStringRejectsShortPayload(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$5\r\nhi\r\n"))

	if _, err := ReadBulkString(r); err == nil {
		t.Fatal("ReadBulkString should reject payloads shorter than declared length")
	}
}
