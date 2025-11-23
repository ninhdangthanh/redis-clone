package command

func HandlePing(ctx *CommandContext, args []string) {
	if len(args) == 2 {
		ctx.Writer.WriteSimpleString(args[1])
	} else {
		ctx.Writer.WriteSimpleString("PONG")
	}
}

func HandleEcho(ctx *CommandContext, args []string) {
	if len(args) != 2 {
		ctx.Writer.WriteError("wrong number of arguments for ECHO")
		return
	}
	ctx.Writer.WriteBulkString([]byte(args[1]))
}

func HandleQuit(ctx *CommandContext, args []string) {
	ctx.Writer.WriteSimpleString("OK")
}
