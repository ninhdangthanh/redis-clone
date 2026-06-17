package command

import (
	"fmt"
	"strings"
)

func HandleSetCommands(ctx *CommandContext, args []string) bool {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return false
	}

	switch strings.ToUpper(args[0]) {
	case "SADD":
		return handleSAdd(ctx, args)
	case "SMEMBERS":
		return handleSMembers(ctx, args)
	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown set command '%s'", args[0]))
		return false
	}
}

func handleSAdd(ctx *CommandContext, args []string) bool {
	if len(args) < 3 {
		ctx.Writer.WriteError("wrong number of arguments for SADD")
		return false
	}

	key := args[1]
	added := 0

	for _, member := range args[2:] {
		wasAdded, err := ctx.Store.SAdd(key, []byte(member))
		if err != nil {
			writeStoreError(ctx, err)
			return false
		}
		if wasAdded {
			added++
		}
	}

	ctx.Writer.WriteInteger(int64(added))
	return true
}

func handleSMembers(ctx *CommandContext, args []string) bool {
	if len(args) != 2 {
		ctx.Writer.WriteError("wrong number of arguments for SMEMBERS")
		return false
	}

	key := args[1]
	members, ok := ctx.Store.SMembers(key)
	if !ok {
		ctx.Writer.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
		return false
	}

	ctx.Writer.WriteArrayHeader(len(members))
	for _, m := range members {
		ctx.Writer.WriteBulkString(m)
	}
	return true
}
