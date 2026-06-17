package command

func HandlePing(ctx *CommandContext, args []string) bool {
	if len(args) > 2 {
		ctx.Writer.WriteError("wrong number of arguments for PING")
		return false
	}
	if len(args) == 2 {
		ctx.Writer.WriteSimpleString(args[1])
	} else {
		ctx.Writer.WriteSimpleString("PONG")
	}
	return true
}

func HandleEcho(ctx *CommandContext, args []string) bool {
	if len(args) != 2 {
		ctx.Writer.WriteError("wrong number of arguments for ECHO")
		return false
	}
	ctx.Writer.WriteBulkString([]byte(args[1]))
	return true
}

func HandleQuit(ctx *CommandContext, args []string) bool {
	if len(args) != 1 {
		ctx.Writer.WriteError("wrong number of arguments for QUIT")
		return false
	}
	ctx.Writer.WriteSimpleString("OK")
	return true
}
