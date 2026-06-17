package command

import (
	"fmt"
	"strconv"
	"strings"
)

func HandleListCommands(ctx *CommandContext, args []string) bool {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return false
	}

	switch strings.ToUpper(args[0]) {
	case "LPUSH":
		return handleLPush(ctx, args)
	case "RPUSH":
		return handleRPush(ctx, args)
	case "LRANGE":
		return handleLRange(ctx, args)
	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown list command '%s'", args[0]))
		return false
	}
}

func handleLPush(ctx *CommandContext, args []string) bool {
	if len(args) < 3 {
		ctx.Writer.WriteError("wrong number of arguments for LPUSH")
		return false
	}

	key := args[1]
	values := make([][]byte, 0, len(args)-2)
	for _, v := range args[2:] {
		values = append(values, []byte(v))
	}
	newLen, ok := ctx.Store.LPush(key, values...)
	if !ok {
		ctx.Writer.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
		return false
	}
	ctx.Writer.WriteInteger(int64(newLen))
	return true
}

func handleRPush(ctx *CommandContext, args []string) bool {
	if len(args) < 3 {
		ctx.Writer.WriteError("wrong number of arguments for RPUSH")
		return false
	}

	key := args[1]
	values := make([][]byte, 0, len(args)-2)
	for _, v := range args[2:] {
		values = append(values, []byte(v))
	}
	newLen, ok := ctx.Store.RPush(key, values...)
	if !ok {
		ctx.Writer.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
		return false
	}
	ctx.Writer.WriteInteger(int64(newLen))
	return true
}

func handleLRange(ctx *CommandContext, args []string) bool {
	if len(args) != 4 {
		ctx.Writer.WriteError("wrong number of arguments for LRANGE")
		return false
	}

	key := args[1]
	start, err := strconv.Atoi(args[2])
	if err != nil {
		ctx.Writer.WriteError("value is not an integer or out of range")
		return false
	}
	stop, err := strconv.Atoi(args[3])
	if err != nil {
		ctx.Writer.WriteError("value is not an integer or out of range")
		return false
	}

	values, ok := ctx.Store.LRange(key, start, stop)
	if !ok {
		ctx.Writer.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
		return false
	}

	ctx.Writer.WriteArrayHeader(len(values))
	for _, v := range values {
		ctx.Writer.WriteBulkString(v)
	}
	return true
}
