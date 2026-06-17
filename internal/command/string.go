package command

import (
	"errors"
	"fmt"
	"redis-clone/internal/store"
	"strconv"
	"strings"
)

func HandleStringCommands(ctx *CommandContext, args []string) bool {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return false
	}

	switch strings.ToUpper(args[0]) {
	case "SET":
		return HandleSet(ctx, args)
	case "GET":
		return HandleGet(ctx, args)
	case "DEL":
		return HandleDel(ctx, args)
	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown string command '%s'", args[0]))
		return false
	}
}

func HandleSet(ctx *CommandContext, args []string) bool {
	if len(args) != 3 && len(args) != 5 {
		ctx.Writer.WriteError("wrong number of arguments for SET")
		return false
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
			if err != nil || sec <= 0 {
				ctx.Writer.WriteError("invalid expire time")
				return false
			}
			ttlMs = sec * 1000

		case "PX":
			ms, err := strconv.ParseInt(exp, 10, 64)
			if err != nil || ms <= 0 {
				ctx.Writer.WriteError("invalid expire time")
				return false
			}
			ttlMs = ms

		case "PXAT":
			expiresAtMs, err := strconv.ParseInt(exp, 10, 64)
			if err != nil || expiresAtMs <= 0 {
				ctx.Writer.WriteError("invalid expire time")
				return false
			}
			if err := ctx.Store.SetAt(key, val, expiresAtMs); err != nil {
				writeStoreError(ctx, err)
				return false
			}
			ctx.Writer.WriteSimpleString("OK")
			return true

		default:
			ctx.Writer.WriteError("syntax error")
			return false
		}
	}

	if err := ctx.Store.Set(key, val, ttlMs); err != nil {
		writeStoreError(ctx, err)
		return false
	}
	ctx.Writer.WriteSimpleString("OK")
	return true
}

func HandleGet(ctx *CommandContext, args []string) bool {
	if len(args) != 2 {
		ctx.Writer.WriteError("wrong number of arguments for GET")
		return false
	}

	key := args[1]
	val, ok := ctx.Store.Get(key)

	if !ok {
		ctx.Writer.WriteNull()
		return true
	}

	ctx.Writer.WriteBulkString(val)
	return true
}

func HandleDel(ctx *CommandContext, args []string) bool {
	if len(args) < 2 {
		ctx.Writer.WriteError("wrong number of arguments for DEL")
		return false
	}

	keys := args[1:]
	deleted := ctx.Store.Del(keys)

	ctx.Writer.WriteInteger(int64(deleted))
	return true
}

func writeStoreError(ctx *CommandContext, err error) {
	switch {
	case errors.Is(err, store.ErrMaxMemory):
		ctx.Writer.WriteError("OOM command not allowed when used memory > 'maxmemory'")
	case errors.Is(err, store.ErrWrongType):
		ctx.Writer.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
	default:
		ctx.Writer.WriteError(err.Error())
	}
}
