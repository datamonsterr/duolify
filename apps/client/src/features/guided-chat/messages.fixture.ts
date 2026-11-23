// Copy for the authored fixture script (e2e harness and screenshots). Not part of the app
// catalogue: the harness and tests register it with `registerFixtureCopy()`.
import { ar, en } from "../design/localization/messages";

const fixtureEn = {
  "fixture.name.q": "What should I call you?",
  "fixture.level.q": "What's your level?",
  "fixture.level.beginner": "Beginner",
  "fixture.level.some": "Some",
  "fixture.level.expert": "Expert",
  "fixture.goals.q": "What would you like to cover?",
  "fixture.goals.speak": "Speaking",
  "fixture.goals.read": "Reading",
  "fixture.goals.write": "Writing",
  "fixture.remind.q": "Want a daily reminder?",
  "fixture.time.q": "What time suits you?",
  "fixture.date.q": "When do you want to start?",
  "fixture.minutes.q": "How many minutes a day?",
  "fixture.notes.q": "Anything else I should know?",
  "fixture.photo.q": "Want to add a picture?",
  "fixture.rating.q": "How do you feel about this?",
  "fixture.email.q": "What's your email?",
  "fixture.confirm.q": "Ready to save these?",
  "fixture.code.q": "Enter the code we sent.",
  "fixture.password.q": "Type your password.",
  "fixture.delete.q": "This can't be undone.",
};

const fixtureAr = {
  "fixture.name.q": "بماذا أناديك؟",
  "fixture.level.q": "ما مستواك؟",
  "fixture.level.beginner": "مبتدئ",
  "fixture.level.some": "متوسط",
  "fixture.level.expert": "متقدم",
  "fixture.goals.q": "ماذا تريد أن نغطي؟",
  "fixture.goals.speak": "المحادثة",
  "fixture.goals.read": "القراءة",
  "fixture.goals.write": "الكتابة",
  "fixture.remind.q": "هل تريد تذكيرًا يوميًا؟",
  "fixture.time.q": "ما الوقت المناسب لك؟",
  "fixture.date.q": "متى تريد أن تبدأ؟",
  "fixture.minutes.q": "كم دقيقة في اليوم؟",
  "fixture.notes.q": "هل من شيء آخر أعرفه؟",
  "fixture.photo.q": "هل تريد إضافة صورة؟",
  "fixture.rating.q": "ما رأيك بهذا؟",
  "fixture.email.q": "ما بريدك الإلكتروني؟",
  "fixture.confirm.q": "هل أحفظ هذه الإجابات؟",
  "fixture.code.q": "أدخل الرمز الذي أرسلناه.",
  "fixture.password.q": "أدخل كلمة المرور للمتابعة.",
  "fixture.delete.q": "لا يمكن التراجع عن هذا.",
};

export function registerFixtureCopy() {
  Object.assign(en, fixtureEn);
  Object.assign(ar, fixtureAr);
}
