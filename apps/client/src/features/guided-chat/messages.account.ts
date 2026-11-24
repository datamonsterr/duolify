import { registerMessages } from "../design/localization/messages";

// Account data copy. Turns stay under 90 characters, buttons under 3 words.
export const accountEn = {
  "g.export.title": "Export data",
  "g.delete.title": "Delete account",
  "g.export.pass.q": "Pick a passphrase. It can't be recovered.",
  "g.export.pass.label": "Export passphrase",
  "g.export.confirm.q": "Download your encrypted export?",
  "g.export.action": "Download",
  "g.export.done": "Encrypted export downloaded.",
  "g.export.error": "We couldn't prepare that. Try again.",
  "g.delete.q":
    "Delete your account? Data goes in 30 days; learners keep copies. No undo.",
  "g.delete.action": "Delete forever",
  "g.delete.error": "We couldn't delete that. Try again.",
  "g.account.reauth": "Verify your identity",
} as const;

export const accountAr: Record<keyof typeof accountEn, string> = {
  "g.export.title": "تصدير البيانات",
  "g.delete.title": "حذف الحساب",
  "g.export.pass.q": "اختر عبارة مرور. لا يمكن استرجاعها.",
  "g.export.pass.label": "عبارة مرور التصدير",
  "g.export.confirm.q": "هل نُنزّل نسختك المشفّرة؟",
  "g.export.action": "تنزيل",
  "g.export.done": "تم تنزيل النسخة المشفّرة.",
  "g.export.error": "تعذّر التجهيز. حاول مرة أخرى.",
  "g.delete.q":
    "هل تحذف حسابك؟ تُحذف بياناتك خلال 30 يومًا ويحتفظ المتعلمون بنسخهم. لا تراجع.",
  "g.delete.action": "احذف نهائيًا",
  "g.delete.error": "تعذّر الحذف. حاول مرة أخرى.",
  "g.account.reauth": "تحقّق من هويتك",
};

registerMessages(accountEn, accountAr);
