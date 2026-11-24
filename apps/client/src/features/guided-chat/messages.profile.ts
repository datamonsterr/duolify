import { registerMessages } from "../design/localization/messages";

// Profile edit and daily goal copy. Turns stay under 90 characters, buttons under 3 words.
export const profileEn = {
  "g.profile.title": "Edit profile",
  "g.profile.topic.q": "Which topic are you on?",
  "g.profile.background.q": "Anything we should know about your background?",
  "g.profile.done": "Profile saved.",
  "g.profile.error": "We couldn't save that. Try again.",
  "g.goal.title": "Daily goal",
  "g.goal.xp.q": "How much XP a day?",
  "g.goal.confirm.q": "Save your daily goal?",
  "g.goal.done": "Daily goal saved.",
  "g.goal.summary": "{minutes} min · {xp} XP",
  "g.goal.reminder_on": "Reminder at {time}",
  "g.goal.reminder_off": "No reminder",
  "g.goal.error": "We couldn't save that. Try again.",
} as const;

export const profileAr: Record<keyof typeof profileEn, string> = {
  "g.profile.title": "تعديل الملف الشخصي",
  "g.profile.topic.q": "ما الموضوع الذي تدرسه؟",
  "g.profile.background.q": "هل من شيء عن خلفيتك نعرفه؟",
  "g.profile.done": "تم حفظ الملف الشخصي.",
  "g.profile.error": "تعذّر الحفظ. حاول مرة أخرى.",
  "g.goal.title": "الهدف اليومي",
  "g.goal.xp.q": "كم نقطة خبرة في اليوم؟",
  "g.goal.confirm.q": "هل نحفظ هدفك اليومي؟",
  "g.goal.done": "تم حفظ الهدف اليومي.",
  "g.goal.summary": "{minutes} دقيقة · {xp} نقطة خبرة",
  "g.goal.reminder_on": "التذكير عند {time}",
  "g.goal.reminder_off": "بلا تذكير",
  "g.goal.error": "تعذّر الحفظ. حاول مرة أخرى.",
};

registerMessages(profileEn, profileAr);
