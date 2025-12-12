package command

import (
	"fmt"
	"strings"
)

func HandleSetCommands(ctx *CommandContext, args []string) {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return
	}

	switch strings.ToUpper(args[0]) {
	case "SADD":
		handleSAdd(ctx, args)
	case "SMEMBERS":
		handleSMembers(ctx, args)
	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown set command '%s'", args[0]))
	}
}

func handleSAdd(ctx *CommandContext, args []string) {
	if len(args) < 3 {
		ctx.Writer.WriteError("wrong number of arguments for SADD")
		return
	}

	key := args[1]
	added := 0

	for _, member := range args[2:] {
		if ctx.Store.SAdd(key, []byte(member)) {
			added++
		}
	}

	ctx.Writer.WriteInteger(int64(added))
}

func handleSMembers(ctx *CommandContext, args []string) {
	if len(args) != 2 {
		ctx.Writer.WriteError("wrong number of arguments for SMEMBERS")
		return
	}

	key := args[1]
	members := ctx.Store.SMembers(key)

	ctx.Writer.WriteArrayHeader(len(members))
	for _, m := range members {
		ctx.Writer.WriteBulkString(m)
	}
}
