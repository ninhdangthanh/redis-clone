package command

import "redis-clone/internal/pubsub"

func HandlePublish(ctx *CommandContext, args []string) bool {
	if len(args) != 3 {
		ctx.Writer.WriteError("wrong number of arguments for PUBLISH")
		return false
	}
	if ctx.PubSub == nil {
		ctx.Writer.WriteInteger(0)
		return true
	}

	receivers := ctx.PubSub.Publish(args[1], []byte(args[2]))
	ctx.Writer.WriteInteger(int64(receivers))
	return true
}

func WriteSubscribeReply(w *RespWriter, kind string, channel string, count int) {
	w.WriteArrayHeader(3)
	w.WriteBulkString([]byte(kind))
	w.WriteBulkString([]byte(channel))
	w.WriteInteger(int64(count))
}

func WritePubSubMessage(w *RespWriter, msg pubsub.Message) {
	w.WriteArrayHeader(3)
	w.WriteBulkString([]byte("message"))
	w.WriteBulkString([]byte(msg.Channel))
	w.WriteBulkString(msg.Payload)
}
