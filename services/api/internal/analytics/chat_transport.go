package analytics

import "context"

type chatTransportKey struct{}

// WithChatTransport marks events produced by a verified Edge chat operation.
func WithChatTransport(ctx context.Context, transport string) context.Context {
	return context.WithValue(ctx, chatTransportKey{}, transport)
}

// ChatTransport reports the transport that reached the API's chat handler.
func ChatTransport(ctx context.Context) string {
	if value, _ := ctx.Value(chatTransportKey{}).(string); value == "websocket" || value == "sse" {
		return value
	}
	return "engine"
}

func chatTransportProperty(input, output map[string]any) {
	if transport, ok := input["chat_transport"].(string); ok {
		switch transport {
		case "websocket", "sse", "engine":
			output["chat_transport"] = transport
		}
	}
}

// ValidChatFallback permits the observed transport transitions with bounded reason codes.
func ValidChatFallback(from, to, reason string) bool {
	if (from != "websocket" || to != "sse") && ((from != "websocket" && from != "sse") || to != "engine") {
		return false
	}
	switch reason {
	case "upgrade_failed", "connection_lost", "edge_unavailable", "disabled":
		return true
	}
	return false
}
