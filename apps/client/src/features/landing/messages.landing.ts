import { registerMessages } from "../design/localization/messages";
// Landing page copy. Loaded with the landing chunk, never in the first-load bundle.
export const landingEn = {
  "landing.title": "Duolify: learn anything, like a game",
  "landing.description":
    "Tell Duolify a topic. AI builds your course. Play short interactive lessons every day.",
  "landing.nav": "Main",
  "landing.sign_in": "Sign in",
  "landing.language": "Language",
  "landing.lang_en": "English",
  "landing.lang_ar": "العربية",
  "landing.theme": "Theme",
  "landing.theme_light": "Switch to light theme",
  "landing.theme_dark": "Switch to dark theme",
  "landing.h1": "Learn anything. Like a game.",
  "landing.lede":
    "Tell us a topic. AI builds your course. Play short interactive lessons every day.",
  "landing.get_started": "Get started",
  "landing.try_shared": "Try a shared course",
  "landing.video": "Duolify in one minute",
  "landing.video_play": "Play video",
  "landing.pillars": "Why Duolify",
  "landing.pillar1.title": "Play to learn",
  "landing.pillar1.body": "Streaks, XP and friends keep you coming back.",
  "landing.pillar2.title": "Interactive video",
  "landing.pillar2.body": "Tap, ask by voice, take notes by voice.",
  "landing.pillar3.title": "Your course, by AI",
  "landing.pillar3.body": "Any topic, your characters, share it.",
  "landing.how": "How it works",
  "landing.step1": "Say what you want to learn",
  "landing.step2": "Get your course in minutes",
  "landing.step3": "Play 5 minutes a day",
  "landing.join": "Start learning",
  "landing.privacy_note":
    "Analytics is anonymous and optional. Codes and tokens never reach analytics.",
} as const;

export const landingAr: Record<keyof typeof landingEn, string> = {
  "landing.title": "دولِفاي: تعلّم أي شيء كأنه لعبة",
  "landing.description":
    "اذكر موضوعًا ويبني الذكاء الاصطناعي دورتك. العب دروسًا تفاعلية قصيرة كل يوم.",
  "landing.nav": "الرئيسية",
  "landing.sign_in": "تسجيل الدخول",
  "landing.language": "اللغة",
  "landing.lang_en": "English",
  "landing.lang_ar": "العربية",
  "landing.theme": "السمة",
  "landing.theme_light": "التبديل إلى السمة الفاتحة",
  "landing.theme_dark": "التبديل إلى السمة الداكنة",
  "landing.h1": "تعلّم أي شيء. كأنه لعبة.",
  "landing.lede":
    "اذكر موضوعًا. يبني الذكاء الاصطناعي دورتك. العب دروسًا تفاعلية قصيرة كل يوم.",
  "landing.get_started": "ابدأ",
  "landing.try_shared": "جرّب دورة مشتركة",
  "landing.video": "دولِفاي في دقيقة",
  "landing.video_play": "شغّل الفيديو",
  "landing.pillars": "لماذا دولِفاي",
  "landing.pillar1.title": "العب لتتعلم",
  "landing.pillar1.body": "السلاسل والنقاط والأصدقاء تبقيك مستمرًا.",
  "landing.pillar2.title": "فيديو تفاعلي",
  "landing.pillar2.body": "اضغط واسأل بصوتك ودوّن ملاحظاتك بصوتك.",
  "landing.pillar3.title": "دورتك بالذكاء الاصطناعي",
  "landing.pillar3.body": "أي موضوع وشخصياتك ومشاركة سهلة.",
  "landing.how": "كيف يعمل",
  "landing.step1": "قل ما تريد أن تتعلمه",
  "landing.step2": "احصل على دورتك خلال دقائق",
  "landing.step3": "العب 5 دقائق يوميًا",
  "landing.join": "ابدأ التعلم",
  "landing.privacy_note":
    "التحليلات مجهولة الهوية واختيارية. الرموز لا تصل إلى التحليلات أبدًا.",
};

registerMessages(landingEn, landingAr);
