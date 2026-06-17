package command

import (
	"strconv"
	"strings"
)

func HandleTTLCommands(ctx *CommandContext, args []string) bool {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return false
	}

	switch strings.ToUpper(args[0]) {
	case "EXPIRE":
		return handleExpire(ctx, args)
	case "TTL":
		return handleTTL(ctx, args)
	default:
		ctx.Writer.WriteError("unknown ttl command")
		return false
	}
}

func handleExpire(ctx *CommandContext, args []string) bool {
	if len(args) != 3 {
		ctx.Writer.WriteError("wrong number of arguments for EXPIRE")
		return false
	}

	seconds, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || seconds < 0 {
		ctx.Writer.WriteError("invalid expire time")
		return false
	}

	if ctx.Store.Expire(args[1], seconds) {
		ctx.Writer.WriteInteger(1)
	} else {
		ctx.Writer.WriteInteger(0)
	}
	return true
}

func handleTTL(ctx *CommandContext, args []string) bool {
	if len(args) != 2 {
		ctx.Writer.WriteError("wrong number of arguments for TTL")
		return false
	}

	ttl := ctx.Store.TTL(args[1])
	ctx.Writer.WriteInteger(ttl)
	return true
}
