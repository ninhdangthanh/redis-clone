package command

import (
	"fmt"
	"strings"
)

func HandleHashCommands(ctx *CommandContext, args []string) bool {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return false
	}

	switch strings.ToUpper(args[0]) {
	case "HSET":
		return handleHSet(ctx, args)
	case "HGET":
		return handleHGet(ctx, args)
	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown hash command '%s'", args[0]))
		return false
	}
}

func handleHSet(ctx *CommandContext, args []string) bool {
	if len(args) != 4 {
		ctx.Writer.WriteError("wrong number of arguments for HSET")
		return false
	}

	key := args[1]
	field := args[2]
	value := []byte(args[3])

	isNew, err := ctx.Store.HSet(key, field, value)
	if err != nil {
		writeStoreError(ctx, err)
		return false
	}
	ctx.Writer.WriteInteger(int64(isNew)) // 1 if new field, 0 if updated
	return true
}

func handleHGet(ctx *CommandContext, args []string) bool {
	if len(args) != 3 {
		ctx.Writer.WriteError("wrong number of arguments for HGET")
		return false
	}

	key := args[1]
	field := args[2]

	val, ok := ctx.Store.HGet(key, field)
	if !ok {
		ctx.Writer.WriteNull()
	} else {
		ctx.Writer.WriteBulkString(val)
	}
	return true
}
