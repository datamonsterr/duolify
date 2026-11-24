import { registerMessages } from "../design/localization/messages";

// Username and add-friend copy. Turns stay under 90 characters, buttons under 3 words.
export const socialEn = {
  "g.username.title": "Choose a username",
  "g.username.q": "Pick a username: 3–20 letters, numbers, _ or .",
  "g.username.label": "Username",
  "g.username.confirm.q": "Use this username?",
  "g.username.action": "Save username",
  "g.username.done": "Username saved.",
  "g.username.error": "We couldn't save that. Try another.",
  "g.friend.title": "Add a friend",
  "g.friend.q": "What's your friend's username?",
  "g.friend.label": "Friend's username",
  "g.friend.confirm.q": "Send a friend request?",
  "g.friend.action": "Send request",
  "g.friend.notfound": "We couldn't find that username.",
  "g.friend.done": "Request sent.",
  "g.friend.error": "We couldn't send that request.",
} as const;

export const socialAr: Record<keyof typeof socialEn, string> = {
  "g.username.title": "اختر اسم مستخدم",
  "g.username.q": "اختر اسم مستخدم: ٣–٢٠ حرفًا أو رقمًا أو _ أو .",
  "g.username.label": "اسم المستخدم",
  "g.username.confirm.q": "هل نستخدم هذا الاسم؟",
  "g.username.action": "احفظ الاسم",
  "g.username.done": "تم حفظ اسم المستخدم.",
  "g.username.error": "تعذّر الحفظ. جرّب اسمًا آخر.",
  "g.friend.title": "إضافة صديق",
  "g.friend.q": "ما اسم مستخدم صديقك؟",
  "g.friend.label": "اسم مستخدم الصديق",
  "g.friend.confirm.q": "هل نرسل طلب صداقة؟",
  "g.friend.action": "أرسل الطلب",
  "g.friend.notfound": "تعذّر العثور على اسم المستخدم هذا.",
  "g.friend.done": "تم إرسال الطلب.",
  "g.friend.error": "تعذّر إرسال الطلب.",
};

registerMessages(socialEn, socialAr);
