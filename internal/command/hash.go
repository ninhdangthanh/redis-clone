package command

import (
	"fmt"
	"strings"
)

func HandleHashCommands(ctx *CommandContext, args []string) {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return
	}

	switch strings.ToUpper(args[0]) {
	case "HSET":
		handleHSet(ctx, args)
	case "HGET":
		handleHGet(ctx, args)
	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown hash command '%s'", args[0]))
	}
}

func handleHSet(ctx *CommandContext, args []string) {
	if len(args) != 4 {
		ctx.Writer.WriteError("wrong number of arguments for HSET")
		return
	}

	key := args[1]
	field := args[2]
	value := []byte(args[3])

	isNew := ctx.Store.HSet(key, field, value)
	ctx.Writer.WriteInteger(int64(isNew)) // 1 if new field, 0 if updated
}

func handleHGet(ctx *CommandContext, args []string) {
	if len(args) != 3 {
		ctx.Writer.WriteError("wrong number of arguments for HGET")
		return
	}

	key := args[1]
	field := args[2]

	val, ok := ctx.Store.HGet(key, field)
	if !ok {
		ctx.Writer.WriteNull()
	} else {
		ctx.Writer.WriteBulkString(val)
	}
}
