package command

func HandleAuth(ctx *CommandContext, args []string) {
	if len(args) != 3 {
		ctx.Writer.WriteError("wrong number of arguments for AUTH")
		return
	}

	user := args[1]
	pass := args[2]

	if ctx.Accounts.Validate(user, pass) {
		ctx.Authenticated = true
		ctx.Username = user
		ctx.Writer.WriteSimpleString("OK")
	} else {
		ctx.Writer.WriteError("invalid username/password")
	}
}
