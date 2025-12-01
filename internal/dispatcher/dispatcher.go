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
		command.HandlePing(ctx, args)

	case "ECHO":
		command.HandleEcho(ctx, args)

	case "AUTH":
		command.HandleAuth(ctx, args)

	case "QUIT":
		command.HandleQuit(ctx, args)

	case "SET", "GET", "DEL":
		command.HandleStringCommands(ctx, args)

	case "LPUSH", "RPUSH", "LRANGE":
		command.HandleListCommands(ctx, args)

	case "SADD", "SMEMBERS":
		command.HandleSetCommands(ctx, args)

	case "HSET", "HGET":
		command.HandleHashCommands(ctx, args)

	case "EXPIRE", "TTL":
		command.HandleTTLCommands(ctx, args)

	default:
		ctx.Writer.WriteError(fmt.Sprintf("unknown command '%s'", cmd))
	}

	return true
}
