package dispatcher

import (
	"fmt"
	"redis-clone/internal/command"
	"strings"
)

func Dispatch(ctx *command.CommandContext, args []string) bool {
	if len(args) == 0 {
		ctx.Writer.WriteError("no command provided")
		return false
	}

	cmd := strings.ToUpper(args[0])

	switch cmd {

	case "PING":
		return command.HandlePing(ctx, args)

	case "ECHO":
		return command.HandleEcho(ctx, args)

	case "QUIT":
		return command.HandleQuit(ctx, args)

	case "SET", "GET", "DEL":
		return command.HandleStringCommands(ctx, args)

	case "LPUSH", "RPUSH", "LRANGE":
		return command.HandleListCommands(ctx, args)

	case "SADD", "SMEMBERS":
		return command.HandleSetCommands(ctx, args)

	case "HSET", "HGET":
		return command.HandleHashCommands(ctx, args)

	case "EXPIRE", "PEXPIREAT", "TTL":
		return command.HandleTTLCommands(ctx, args)

	case "PUBLISH":
		return command.HandlePublish(ctx, args)

	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown command '%s'", cmd))
		return false
	}
}
