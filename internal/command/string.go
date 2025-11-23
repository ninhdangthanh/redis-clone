package command

import (
	"fmt"
	"strconv"
	"strings"
)

func HandleStringCommands(ctx *CommandContext, args []string) {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return
	}

	switch strings.ToUpper(args[0]) {
	case "SET":
		HandleSet(ctx, args)
	case "GET":
		HandleGet(ctx, args)
	case "DEL":
		HandleDel(ctx, args)
	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown string command '%s'", args[0]))
	}
}

func HandleSet(ctx *CommandContext, args []string) {
	if len(args) != 3 && len(args) != 5 {
		ctx.Writer.WriteError("wrong number of arguments for SET")
		return
	}

	key := args[1]
	val := []byte(args[2])
	var ttlMs int64 = 0

	if len(args) == 5 {
		flag := strings.ToUpper(args[3])
		exp := args[4]

		switch flag {
		case "EX":
			sec, err := strconv.ParseInt(exp, 10, 64)
			if err != nil || sec < 0 {
				ctx.Writer.WriteError("invalid expire time")
				return
			}
			ttlMs = sec * 1000

		case "PX":
			ms, err := strconv.ParseInt(exp, 10, 64)
			if err != nil || ms < 0 {
				ctx.Writer.WriteError("invalid expire time")
				return
			}
			ttlMs = ms

		default:
			ctx.Writer.WriteError("syntax error")
			return
		}
	}

	ctx.Store.Set(ctx.Username, key, val, ttlMs)
	ctx.Writer.WriteSimpleString("OK")
}

func HandleGet(ctx *CommandContext, args []string) {
	if len(args) != 2 {
		ctx.Writer.WriteError("wrong number of arguments for GET")
		return
	}

	key := args[1]
	val, ok := ctx.Store.Get(ctx.Username, key)

	if !ok {
		ctx.Writer.WriteNull()
		return
	}

	ctx.Writer.WriteBulkString(val)
}

func HandleDel(ctx *CommandContext, args []string) {
	if len(args) < 2 {
		ctx.Writer.WriteError("wrong number of arguments for DEL")
		return
	}

	keys := args[1:]
	deleted := ctx.Store.Del(ctx.Username, keys)

	ctx.Writer.WriteInteger(int64(deleted))
}
