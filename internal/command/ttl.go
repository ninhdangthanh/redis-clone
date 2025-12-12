package command

func HandleTTLCommands(ctx *CommandContext, args []string) {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return
	}

	if len(args) != 2 {
		ctx.Writer.WriteError("wrong number of arguments for TTL")
		return
	}

	ttl := ctx.Store.TTL(args[1])
	ctx.Writer.WriteInteger(int64(ttl))
}
