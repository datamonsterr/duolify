package chat

import "strings"

// Placeholder self-harm copy (ADR 0025). The owner approves the real wording and the support
// resources per locale in issue #423; until then this is a short, non-clinical message that names
// no organisation and no phone number. Replace the strings, not the code path.
var safeResponseCopy = map[string]string{
	"en": "It sounds like things are really hard right now. You are not alone, and talking to someone you trust, or to a local emergency or support line, can help. Your lessons are here whenever you are ready.",
	"vi": "Có vẻ như bạn đang trải qua một giai đoạn rất khó khăn. Bạn không đơn độc, và nói chuyện với người bạn tin tưởng hoặc gọi đường dây hỗ trợ khẩn cấp tại địa phương có thể giúp ích. Bài học vẫn ở đây khi bạn sẵn sàng.",
	"es": "Parece que ahora mismo las cosas son muy difíciles. No estás solo o sola, y hablar con alguien de confianza o con una línea local de emergencia o apoyo puede ayudar. Tus lecciones estarán aquí cuando quieras.",
}

// SafeResponseCopy picks the copy for an Accept-Language header or locale tag; English is the default.
func SafeResponseCopy(locale string) string {
	tag := strings.ToLower(strings.TrimSpace(locale))
	if len(tag) >= 2 {
		if c, ok := safeResponseCopy[tag[:2]]; ok {
			return c
		}
	}
	return safeResponseCopy["en"]
}
