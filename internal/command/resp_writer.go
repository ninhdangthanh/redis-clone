package command

import (
	"bufio"
	"fmt"
)

type RespWriter struct {
	w *bufio.Writer
}

func NewRespWriter(w *bufio.Writer) *RespWriter {
	return &RespWriter{w: w}
}

func (rw *RespWriter) WriteError(msg string) {
	fmt.Fprintf(rw.w, "-%s\r\n", msg)
	rw.w.Flush()
}

func (rw *RespWriter) WriteSimpleString(msg string) {
	fmt.Fprintf(rw.w, "+%s\r\n", msg)
	rw.w.Flush()
}

func (rw *RespWriter) WriteBulkString(val []byte) {
	fmt.Fprintf(rw.w, "$%d\r\n%s\r\n", len(val), val)
	rw.w.Flush()
}

func (rw *RespWriter) WriteNull() {
	fmt.Fprint(rw.w, "$-1\r\n")
	rw.w.Flush()
}

func (rw *RespWriter) WriteInteger(val int64) {
	fmt.Fprintf(rw.w, ":%d\r\n", val)
	rw.w.Flush()
}

func (rw *RespWriter) WriteArrayHeader(n int) {
	fmt.Fprintf(rw.w, "*%d\r\n", n)
	rw.w.Flush()
}
