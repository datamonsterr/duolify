package analytics

import enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"

// WithOpenRouterTransport adds routing facts to an existing $ai_generation event.
func WithOpenRouterTransport(props map[string]any, transport *enginev1.OpenRouterTransport) map[string]any {
	switch transport.GetRoute() {
	case enginev1.OpenRouterTransport_ROUTE_GATEWAY:
		props["transport_route"] = "gateway"
	case enginev1.OpenRouterTransport_ROUTE_DIRECT:
		props["transport_route"] = "direct"
	default:
		return props
	}
	switch transport.GetGatewayCacheStatus() {
	case enginev1.OpenRouterTransport_CACHE_STATUS_HIT:
		props["gateway_cache_status"] = "hit"
	case enginev1.OpenRouterTransport_CACHE_STATUS_MISS:
		props["gateway_cache_status"] = "miss"
	case enginev1.OpenRouterTransport_CACHE_STATUS_BYPASS:
		props["gateway_cache_status"] = "bypass"
	default:
		props["gateway_cache_status"] = "unknown"
	}
	props["gateway_fallback"] = transport.GetGatewayFallback()
	switch transport.GetGatewayReason() {
	case enginev1.OpenRouterTransport_REASON_TIMEOUT:
		props["gateway_reason"] = "timeout"
	case enginev1.OpenRouterTransport_REASON_UNAVAILABLE:
		props["gateway_reason"] = "unavailable"
	case enginev1.OpenRouterTransport_REASON_TRANSPORT_ERROR:
		props["gateway_reason"] = "transport_error"
	default:
		props["gateway_reason"] = "none"
	}
	return props
}
