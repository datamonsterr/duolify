// Sign-in script copy. Turns stay under 90 characters, buttons under 3 words.
export const signinEn = {
  "g.signin.email.q": "What's your email address?",
  "g.signin.email.label": "Email address",
  "g.signin.code.label": "One-time code",
  "g.signin.age.q": "Confirm to get your code.",
  "g.signin.age.check": "I am 16 or older",
  "g.signin.send_code": "Send code",
  "g.signin.code.q": "Enter the one-time code from your email.",
  "g.signin.sign_in": "Sign in",
  "g.signin.resend": "Resend code",
  "g.signin.sent": "Code sent. It expires in 10 minutes.",
  "g.signin.title": "Sign in",
  "g.signin.back": "Back to welcome",
} as const;

export const signinAr: Record<keyof typeof signinEn, string> = {
  "g.signin.email.q": "ما بريدك الإلكتروني؟",
  "g.signin.email.label": "عنوان البريد الإلكتروني",
  "g.signin.code.label": "رمز الاستخدام لمرة واحدة",
  "g.signin.age.q": "أكّد لنرسل لك الرمز.",
  "g.signin.age.check": "عمري 16 عامًا أو أكثر",
  "g.signin.send_code": "أرسل الرمز",
  "g.signin.code.q": "أدخل الرمز المؤقت من بريدك.",
  "g.signin.sign_in": "تسجيل الدخول",
  "g.signin.resend": "أعد إرسال الرمز",
  "g.signin.sent": "أُرسل الرمز. ينتهي خلال 10 دقائق.",
  "g.signin.title": "تسجيل الدخول",
  "g.signin.back": "العودة للترحيب",
};
