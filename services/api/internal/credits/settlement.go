package credits

import (
	"fmt"
	"math"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

// CalculateSettlement prices actual attempt counts using only pinned terms.
// Failed attempts consume operational USD, but only delivered components consume Credits.
func CalculateSettlement(q pricing.Quote, usage []*enginev1.Usage) (Reservation, error) {
	result := Reservation{PricingVersion: q.PricingVersion, State: "released"}
	var contentNanos, characters int64
	var delivered bool
	totals := map[string][4]int64{}
	for _, u := range usage {
		if u == nil {
			return result, ErrUsage
		}
		if u.Component != "content" && u.Component != "audio" {
			return result, ErrUsage
		}
		if u.Cached {
			if u.InputTokens != 0 || u.OutputTokens != 0 || u.CacheWriteTokens != 0 || u.CacheReadTokens != 0 || u.NarrationCharacters != 0 {
				return result, ErrUsage
			}
			continue
		}
		if u.Attempt == 0 || int64(u.Attempt) > q.Bounds["max_attempts"] || int64(u.LatencyMs) > q.Bounds["timeout_ms"] {
			return result, ErrUsage
		}
		var cost int64
		if u.Component == "audio" {
			if u.Provider != "elevenlabs" || u.Model != q.Policy.AudioModel || int64(u.NarrationCharacters) > q.Bounds["narration_characters"] {
				return result, ErrUsage
			}
			cost = product(int64(u.NarrationCharacters), q.Policy.AudioUSDNanosPerCharacter)
			if u.Delivered {
				characters += int64(u.NarrationCharacters)
			}
		} else {
			rate := q.Rate
			if u.Provider != rate.Provider || u.Model != rate.Model {
				fallbackKey := q.Policy.FallbackModels[q.Input.Model]
				fallback, ok := q.Policy.Models[fallbackKey]
				if !ok || u.Provider != fallback.Provider || u.Model != fallback.Model || !u.FallbackUsed {
					return result, ErrUsage
				}
				rate = fallback
			}
			key := fmt.Sprintf("%s:%d", u.Component, u.Attempt)
			total := totals[key]
			total[0] += int64(u.InputTokens)
			total[1] += int64(u.OutputTokens)
			total[2] += int64(u.CacheWriteTokens)
			total[3] += int64(u.CacheReadTokens)
			totals[key] = total
			if total[0]+total[2]+total[3] > q.Bounds["input_tokens"] || total[1] > q.Bounds["output_tokens"] {
				return result, ErrUsage
			}
			cost = sumProducts([2]int64{int64(u.InputTokens), rate.Input}, [2]int64{int64(u.OutputTokens), rate.Output}, [2]int64{int64(u.CacheWriteTokens), rate.CacheWrite}, [2]int64{int64(u.CacheReadTokens), rate.CacheRead})
			if u.Delivered {
				delivered = true
				contentNanos += cost
			}
		}
		// Unknown usage reserves its whole worst-case attempt cost, never zero.
		if u.UsageUnavailable {
			if u.Component == "audio" {
				cost = product(q.Input.NarrationCharacters, q.Policy.AudioUSDNanosPerCharacter)
			} else {
				cost = attemptCost(q)
			}
		}
		if cost < 0 || cost > q.Bounds["max_cost_nanos"]-result.CostNanos {
			return result, ErrUsage
		}
		result.CostNanos += cost
	}
	if characters > q.Bounds["narration_characters"] {
		return result, ErrUsage
	}
	if delivered {
		if q.Policy.USDNanosPerCredit <= 0 {
			return result, ErrUsage
		}
		result.ContentSpent = min(q.ContentCeiling, max(q.ContentMinimum, ceil(contentNanos, q.Policy.USDNanosPerCredit)))
		for _, u := range usage {
			if u.Component == "content" && u.Delivered && !u.Cached && u.UsageUnavailable {
				result.ContentSpent = q.ContentCeiling
				break
			}
		}
	}
	if characters > 0 {
		if q.NarrationDenominator <= 0 {
			return result, ErrUsage
		}
		result.AudioSpent = min(q.AudioCeiling, ceil(characters*q.NarrationNumerator, q.NarrationDenominator))
	}
	if result.ContentSpent+result.AudioSpent > 0 {
		result.State = "settled"
	}
	return result, nil
}
func ceil(n, d int64) int64 {
	if n <= 0 {
		return 0
	}
	return n/d + boolInt(n%d != 0)
}
func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
func attemptCost(q pricing.Quote) int64 {
	rate := q.Rate
	fallback := q.Policy.Models[q.Policy.FallbackModels[q.Input.Model]]
	input := max(rate.Input, rate.CacheWrite, rate.CacheRead, fallback.Input, fallback.CacheWrite, fallback.CacheRead)
	output := max(rate.Output, fallback.Output)
	return sumProducts([2]int64{q.Bounds["input_tokens"], input}, [2]int64{q.Bounds["output_tokens"], output})
}
func reservationCost(q pricing.Quote) int64 {
	return sumProducts([2]int64{attemptCost(q), q.Bounds["max_attempts"]}, [2]int64{product(q.Input.NarrationCharacters, q.Policy.AudioUSDNanosPerCharacter), q.Bounds["max_attempts"]})
}

// Reject config/usage arithmetic overflow rather than wrapping operational cost.
func product(factors ...int64) int64 {
	value := int64(1)
	for _, factor := range factors {
		if factor < 0 {
			return -1
		}
		if factor == 0 {
			value = 0
			continue
		}
		if value > math.MaxInt64/factor {
			return -1
		}
		value *= factor
	}
	return value
}
func sumProducts(pairs ...[2]int64) int64 {
	total := int64(0)
	for _, pair := range pairs {
		value := product(pair[0], pair[1])
		if value < 0 || value > math.MaxInt64-total {
			return -1
		}
		total += value
	}
	return total
}
