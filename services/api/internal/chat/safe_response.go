package chat

import "strings"

// Owner-approved self-harm copy (ADR 0025), wording approved 2026-11-25 (issue #423). No hotline
// numbers or organisations yet: the copy points to local emergency services and a trusted person.
// The owner reviews any wording change. Replace the strings, not the code path.
var safeResponseCopy = map[string]string{
	"en": "I'm really sorry you're going through this. You deserve support from a real person. If you might be in danger, please contact your local emergency services now. You can also reach out to someone you trust, like a friend, family member or a doctor. I'm here to keep helping with your learning whenever you want.",
	"vi": "Mình rất tiếc khi bạn đang trải qua điều này. Bạn xứng đáng được một người thật hỗ trợ. Nếu bạn có thể đang gặp nguy hiểm, hãy liên hệ ngay với dịch vụ cấp cứu tại địa phương. Bạn cũng có thể nói chuyện với một người bạn tin tưởng, như bạn bè, người thân hoặc bác sĩ. Mình vẫn ở đây để giúp việc học của bạn bất cứ khi nào bạn muốn.",
	"es": "Lamento mucho que estés pasando por esto. Mereces el apoyo de una persona real. Si podrías estar en peligro, contacta ahora con los servicios de emergencia de tu zona. También puedes hablar con alguien de confianza, como un amigo, un familiar o un médico. Sigo aquí para ayudarte con tu aprendizaje cuando quieras.",
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
