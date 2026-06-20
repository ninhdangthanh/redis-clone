package command

import (
	"fmt"
	"redis-clone/internal/store"
	"strings"
)

// HandleInfo returns a diagnostics snapshot of the current Store.
//
// Supported sections are METRICS, CONFIG, and STATE. Without a section, it
// returns all sections in a Redis-compatible bulk string.
func HandleInfo(ctx *CommandContext, args []string) bool {
	if !ctx.Authenticated {
		ctx.Writer.WriteError("NOAUTH Authentication required")
		return false
	}
	if len(args) > 2 {
		ctx.Writer.WriteError("wrong number of arguments for INFO")
		return false
	}

	section := "ALL"
	if len(args) == 2 {
		section = strings.ToUpper(args[1])
	}

	info := ctx.Store.Info()
	var out strings.Builder
	switch section {
	case "ALL":
		writeMetricsInfo(&out, info)
		writeConfigInfo(&out, info)
		writeStateInfo(&out, info)
	case "METRICS":
		writeMetricsInfo(&out, info)
	case "CONFIG":
		writeConfigInfo(&out, info)
	case "STATE":
		writeStateInfo(&out, info)
	default:
		ctx.Writer.WriteError("unsupported INFO section")
		return false
	}

	ctx.Writer.WriteBulkString([]byte(out.String()))
	return true
}

func writeMetricsInfo(out *strings.Builder, info store.Info) {
	fmt.Fprintln(out, "# Metrics")
	fmt.Fprintf(out, "key_count:%d\n", info.Metrics.KeyCount)
	fmt.Fprintf(out, "memory_usage_bytes:%d\n", info.Metrics.MemoryUsageBytes)
	fmt.Fprintf(out, "expiring_keys:%d\n", info.Metrics.ExpiringKeys)
}

func writeConfigInfo(out *strings.Builder, info store.Info) {
	fmt.Fprintln(out, "# Config")
	fmt.Fprintf(out, "maxmemory:%d\n", info.Config.MaxMemory)
	fmt.Fprintf(out, "maxmemory_policy:%s\n", info.Config.EvictionPolicy)
}

func writeStateInfo(out *strings.Builder, info store.Info) {
	fmt.Fprintln(out, "# State")
	fmt.Fprintf(out, "string_keys:%d\n", info.State.StringKeys)
	fmt.Fprintf(out, "list_keys:%d\n", info.State.ListKeys)
	fmt.Fprintf(out, "set_keys:%d\n", info.State.SetKeys)
	fmt.Fprintf(out, "hash_keys:%d\n", info.State.HashKeys)
}
