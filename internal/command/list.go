package command

import (
	"fmt"
	"strconv"
	"strings"
)

func HandleListCommands(ctx *CommandContext, args []string) {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return
	}

	switch strings.ToUpper(args[0]) {
	case "LPUSH":
		handleLPush(ctx, args)
	case "RPUSH":
		handleRPush(ctx, args)
	case "LRANGE":
		handleLRange(ctx, args)
	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown list command '%s'", args[0]))
	}
}

func handleLPush(ctx *CommandContext, args []string) {
	if len(args) < 3 {
		ctx.Writer.WriteError("wrong number of arguments for LPUSH")
		return
	}

	key := args[1]
	for _, v := range args[2:] {
		ctx.Store.LPush(key, []byte(v))
	}

	newLen := len(ctx.Store.LRange(key, 0, -1))
	ctx.Writer.WriteInteger(int64(newLen))
}

func handleRPush(ctx *CommandContext, args []string) {
	if len(args) < 3 {
		ctx.Writer.WriteError("wrong number of arguments for RPUSH")
		return
	}

	key := args[1]
	for _, v := range args[2:] {
		ctx.Store.RPush(key, []byte(v))
	}

	newLen := len(ctx.Store.LRange(key, 0, -1))
	ctx.Writer.WriteInteger(int64(newLen))
}

func handleLRange(ctx *CommandContext, args []string) {
	if len(args) != 4 {
		ctx.Writer.WriteError("wrong number of arguments for LRANGE")
		return
	}

	key := args[1]
	start, _ := strconv.Atoi(args[2])
	stop, _ := strconv.Atoi(args[3])

	values := ctx.Store.LRange(key, start, stop)

	ctx.Writer.WriteArrayHeader(len(values))
	for _, v := range values {
		ctx.Writer.WriteBulkString(v)
	}
}
