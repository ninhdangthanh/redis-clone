package command

import (
	"bufio"
	"bytes"
	"testing"
)

func TestRespWriterWritesRESPTypes(t *testing.T) {
	var buf bytes.Buffer
	w := NewRespWriter(bufio.NewWriter(&buf))

	w.WriteSimpleString("OK")
	w.WriteError("ERR bad")
	w.WriteBulkString([]byte("hello"))
	w.WriteNull()
	w.WriteInteger(42)
	w.WriteArrayHeader(2)

	want := "+OK\r\n-ERR bad\r\n$5\r\nhello\r\n$-1\r\n:42\r\n*2\r\n"
	if got := buf.String(); got != want {
		t.Fatalf("RESP output = %q, want %q", got, want)
	}
}
