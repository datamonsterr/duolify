import { guidedEn } from "../../guided-chat/messages";
import { onboardingEn } from "../../onboarding/messages";
import { mascotEn } from "../../../components/mascot/messages";
// Stable IDs and complete sentences. Feature content stays data, never a message key.
// Plural branches follow Intl/CLDR categories; every branch includes its own sentence.
export type PluralMessage = { other: string } & Partial<
  Record<Intl.LDMLPluralRule, string>
>;
export type Message = string | PluralMessage;
// Review strings use authored English fallbacks until translated catalogues are supplied.
export const reviewMessages = {
  "review.title": "Reviews",
  "review.saved": "Review saved.",
  "review.hidden":
    "Your stars are saved. Your review text is hidden while it awaits approval or needs changes.",
  "review.deleted": "Review deleted.",
  "review.loading": "Loading reviews…",
  "review.empty": "No reviews yet. Share your view after learning.",
  "review.owner": "You can't review your own course.",
  "review.unlock": {
    one: "Reviews open after {count} lesson",
    other: "Reviews open after {count} lessons",
  },
  "review.progress":
    "You've finished {completed}. Complete {needed} more to share your view.",
  "review.yours": "Your review",
  "review.yours_version": "Your review · on v{version}",
  "review.rating": "Star rating",
  "review.star_label": { one: "{count} star", other: "{count} stars" },
  "review.text": "Review text",
  "review.text_help":
    "Optional · {count} / {max} characters. Text is checked before it appears.",
  "review.post": "Post review",
  "review.save": "Save review",
  "review.saving": "Saving…",
  "review.delete": "Delete review",
  "review.list": "Course reviews",
  "review.item": "{author} · {stars} · on v{version}",
  "review.hidden_text": "Review text is hidden.",
  "review.count": { one: "{count} review", other: "{count} reviews" },
  "review.confirm_title": "Delete your review?",
  "review.confirm_body": "This removes your stars and review from this course.",
  "review.confirm": "Confirm delete",
  "review.cancel": "Cancel",
  "review.retry": "Retry",
} satisfies Record<string, Message>;
export const en = {
  ...mascotEn,
  ...onboardingEn,
  ...guidedEn,
  ...reviewMessages,
  "checkin.granted": "+{amount} Credits · daily check-in",
  "checkin.full": "Your check-in bank is full",
  "checkin.balance": "Current balance: {balance} Credits.",
  "checkin.capped":
    "Bank cap reached ({cap}). Current balance: {balance} Credits.",
  "checkin.source": "Check-in Credits",
  "outline.default_goal": "Describe what you learned.",
  "outline.goal_for": "Goal for {name}",
  "outline.goal_error": "Enter a lesson goal.",
  "outline.expand": "Expand {name}",
  "outline.collapse": "Collapse {name}",
  "player.more_controls": "More controls",
  "settings.sec_look": "Look and language",
  "settings.sec_audio": "Lessons and audio",
  "settings.sec_goals": "Goals and reminders",
  "settings.sec_privacy": "Privacy and data",
  "settings.sec_account": "Account",
  "settings.text_size": "Text size",
  "settings.narration": "Narration",
  "settings.speed": "Speed",
  "settings.captions": "Captions",
  "settings.autoplay": "Autoplay",
  "settings.rotate_full": "Full screen on rotate",
  "settings.ui_sounds": "UI sounds",
  "settings.haptics": "Haptics",
  "settings.on": "On",
  "settings.off": "Off",
  "settings.locale": "Locale",
  "settings.time_zone": "Time zone",
  "settings.daily_xp": "Daily XP goal",
  "settings.daily_minutes": "Daily minutes",
  "settings.reminders": "Reminders",
  "settings.reminder_time": "Reminder time",
  "settings.analytics": "Analytics",
  "settings.save": "Save settings",
  "settings.saved": "Settings saved",
  "settings.back": "Back",
  "settings.loading": "Loading…",
  "settings.retry_load": "Retry",
  "settings.read_error": "Could not read settings. Connect and retry.",
  "settings.save_error": "Could not save settings. Retry.",
  "settings.characters": "Private characters",
  "settings.sessions": "Manage sessions",
  "path.start_short": "Start",
  "path.review": "Lesson {index}, {name}, completed. Review",
  "path.locked_help":
    "Lesson {index}, {name}, locked. Pass the previous lesson test first",
  "course.position": "Unit {unit} · Lesson {lesson} of {count}",
  "demo.remaining": "Brief · 1 question left",
  "demo.skip_hint": "You can skip any question. Nothing starts automatically.",
  "demo.form": "Course brief form",
  "demo.back": "Back to chat",
  "demo.level": "Starting level",
  "demo.beginner": "Beginner",
  "demo.intermediate": "Intermediate",
  "demo.advanced": "Advanced",
  "demo.reminders": "Send learning reminders",
  "demo.topic_error": "Enter a topic before confirming.",
  "demo.confirm_error": "Could not confirm. Your draft is kept; try again.",
  "demo.skipped": "Skipped",
  "demo.you": "You",
  "demo.thanks": "Thanks. Review your brief and confirm when you're ready.",
  "demo.applied": "Proposal applied. Undo available.",
  "demo.discarded": "Proposal discarded. Outline unchanged.",
  "demo.show": "Show suggestion",
  "demo.keyboard": "Sheet keyboard example",
  "demo.paused": "Player paused",
  "demo.ready": "Player ready",
  "demo.open_keyboard": "Open keyboard sheet",
  "demo.sheet": "Keyboard sheet",
  "demo.sheet_hint":
    "Opening a sheet pauses the player and keeps the slide position.",
  "demo.note": "Your note",
  "demo.last": "Last action",
  "course.current": "Current",
  "course.unavailable": "Course unavailable",
  "course.progress": "Completed lessons in {name}",
  "course.completed": {
    one: "{completed} of {count} lesson completed",
    other: "{completed} of {count} lessons completed",
  },
  "path.loading": "Loading your path…",
  "path.error": "Could not load this course.",
  "path.title": "YOUR LEARNING PATH",
  "path.explore": "KEEP EXPLORING",
  "path.start": "Start lesson: {name}",
  "path.locked": "Locked",
  "path.completed": "Completed",
  "path.ready": "Ready to start",
  "nav.notes_short": "Notes",
  "settings.title": "Settings",
  "settings.language": "Language",
  "settings.appearance": "Appearance",
  "settings.system": "System",
  "settings.light": "Light",
  "settings.dark": "Dark",
  "settings.motion": "Reduce motion",
  "settings.reset": "Reset preferences",
  "settings.local": "Preferences apply to this session.",
  "settings.error":
    "Could not save preferences. Your choice still applies here. Retry or reset preferences.",
  "settings.retry": "Retry saving preferences",
  "settings.preview": "Formatting preview",
  "settings.date": "Local date: {date}",
  "settings.price": "Price example: {price}",
  "credits.count": { one: "{count} credit", other: "{count} credits" },
  "common.preparing": "Preparing…",
  "common.close": "Close",
  "common.close_named": "Close {name}",
  "common.undo": "Undo",
  "chat.title": "Guided chat",
  "chat.steps": "Chat steps",
  "chat.step": "Step {current} of {total}",
  "chat.form": "Prefer a form?",
  "chat.transcript": "Chat transcript",
  "chat.empty": "No messages yet. You can start with an answer.",
  "chat.composing": " (composing)",
  "chat.new_messages": "New assistant messages",
  "chat.typing": "Duolify is typing…",
  "chat.wait": "Sending is unavailable while a reply is being prepared.",
  "chat.offline":
    "Offline. Your draft stays here; use the form or retry when connected.",
  "chat.retry": "Retry chat",
  "chat.error": "Chat is unavailable",
  "chat.recovery": "Your answers stay here. Use the form to keep going.",
  "chat.replies": "Quick replies",
  "chat.multiple_replies": "Quick replies, choose any",
  "chat.skip": "Skip",
  "chat.answer": "Your answer",
  "chat.send": "Send answer",
  "summary.intro": "Updates as you chat. You can edit every line.",
  "summary.wait": "Waiting for your answer. You can leave this blank.",
  "summary.pending": "Confirming brief…",
  "summary.confirmed": "Brief confirmed",
  "summary.unsaved": "Unsaved changes",
  "summary.review": "Review before confirming",
  "summary.disclosure":
    "Nothing is saved or charged until you explicitly confirm in the course flow.",
  "summary.confirm": "Confirm brief",
  "summary.topic": "Topic",
  "summary.goal": "Goal",
  "summary.include": "Include",
  "summary.skip": "Skip topics",
  "summary.viewing": "Viewing",
  "summary.lessons": "Lessons",
  "outline.moved": "{name} moved to position {position}",
  "outline.edited": "Title edited. Unsaved changes.",
  "outline.removed": "Item removed. Undo available.",
  "outline.new_lesson": "New lesson",
  "outline.new_unit": "New unit",
  "outline.lesson_added": "Lesson added. Unsaved changes.",
  "outline.unit_added": "Unit added. Unsaved changes.",
  "outline.untitled_lesson": "Untitled lesson",
  "outline.untitled_unit": "Untitled unit",
  "outline.reorder": "Reorder {name}",
  "outline.drag_help": "Drag, use arrow keys or move buttons",
  "outline.move_up": "Move {name} up",
  "outline.move_down": "Move {name} down",
  "outline.first": "Unavailable, first position",
  "outline.up": "Move up",
  "outline.last": "Unavailable, last position",
  "outline.down": "Move down",
  "outline.remove_named": "Remove {name}",
  "outline.remove": "Remove",
  "outline.title": "Shape your outline",
  "outline.undone": "Change undone. Unsaved changes.",
  "outline.intro":
    "Sample outline. Drag units or lessons, use arrow keys on a handle, or use move buttons. Lesson content is prepared later.",
  "outline.empty": "No units yet. Add a unit to start.",
  "outline.unit": "Unit {index}",
  "outline.lesson": "Lesson {unit}.{lesson}",
  "outline.title_for": "Title for {name}",
  "outline.unit_error": "Enter a unit title.",
  "outline.lesson_error": "Enter a lesson title.",
  "outline.add_lesson": "Add a lesson to {name}",
  "outline.add_unit": "Add a unit",
  "proposal.title": "Proposed outline changes",
  "proposal.suggestion": "Here's my suggestion",
  "proposal.added": "Added",
  "proposal.removed": "Removed",
  "proposal.moved": "Moved",
  "proposal.retitled": "Retitled",
  "proposal.apply": "Apply",
  "proposal.discard": "Discard",
  "nav.learn": "Learn",
  "courses.collections": "Course collections",
  "courses.create": "Create a course",
  "courses.discover_tab": "Discover",
  "courses.mine_tab": "My courses",
  "courses.refresh": "Refresh courses",
  "courses.retry": "Retry",
  "courses.search": "Search courses",
  "nav.courses": "Courses",
  "nav.friends": "Friends",
  "nav.leagues": "Leagues",
  "nav.notes": "Quick notes",
  "nav.bookmarks": "Bookmarks",
  "nav.marketplace": "Marketplace",
  "nav.credits": "Credits",
  "nav.profile": "Profile",
  "nav.settings": "Settings",
  "nav.moderation_notices": "Moderation notices",
  "moderation.course_banner_title": "Moderation notice",
  "moderation.course_banner_attention":
    "This course has a moderation notice that needs your attention.",
  "moderation.course_banner_generic": "This course has a moderation notice.",
  "moderation.course_banner_link": "View moderation notices",
  "nav.more": "More",
  "nav.streak": "Your streak",
  "shell.learning": "Learning",
  "shell.account": "Account",
  "shell.preview": "Preview",
  "shell.streak_example": "Example: 3 of 7 days this week",
  "shell.streak_example_label": "Example weekly streak progress",
  "shell.goal_example": "Example: 10 of 20 XP today",
  "shell.goal_example_label": "Example daily goal progress",
  "shell.skip": "Skip to content",
  "shell.main": "Main",
  "shell.more": "More destinations",
  "lang.en": "English",
  "lang.ar": "العربية",
  "shell.dark_theme": "Dark theme",
  "shell.stat_streak": "{count} day streak",
  "shell.stat_xp": "{done} of {goal} XP today",
  "shell.stat_xp_none": "XP today",
  "shell.stat_credits": "{count} credits",
  "shell.balance": "Balance unavailable",
  "shell.progress": "Your progress",
  "shell.habit": "Build a habit, one lesson at a time.",
  "shell.goal": "Daily goal",
  "shell.pace": "Your learning, your pace.",
  "demo.title": "Let's shape your course",
  "demo.path": "Brief → Preview → Outline → Create",
  "demo.brief": "Course brief",
  "demo.open_brief": "Open course brief",
  "demo.outline_chat": "Outline chat",
  "demo.open_outline": "Open outline chat",
  "demo.city": "City balcony",
  "demo.both": "Both",
  "demo.dark_sky": "Dark sky is fine",
  "demo.unsure": "Not sure",
  "tutor.entry": "Ask the tutor",
  "tutor.title": "Tutor",
  "tutor.region": "Tutor chat",
  "tutor.close": "Close tutor chat",
  "tutor.empty_title": "Ask anything about this course",
  "tutor.starter_1": "Explain the main idea of this course simply",
  "tutor.starter_2": "Give me another example from my current lesson",
  "tutor.starter_3": "Quiz me on what I have learned so far",
  "tutor.context_toggle": "Context",
  "tutor.context_title": "What the tutor uses",
  "tutor.context_body":
    "This course, your lessons so far and your learner profile.",
  "tutor.context_not": "Not used: your notes, friends or other courses.",
  "tutor.message_label": "Message",
  "tutor.class_label": "Reply length",
  "tutor.class_short": "Short reply",
  "tutor.class_long": "Long reply",
  "tutor.estimate": "{reply} up to {credits} credits",
  "tutor.estimate_loading": "Checking the cost\u2026",
  "tutor.estimate_hint": "Type a question to see its estimate",
  "tutor.balance": "Daily free {daily} \u00b7 Check-in and paid {paid}",
  "tutor.send": "Send",
  "tutor.send_offline": "Send unavailable offline",
  "tutor.stop": "Stop reply",
  "tutor.writing": "Writing\u2026",
  "tutor.answering": "Tutor is answering",
  "tutor.replied": "Tutor replied",
  "tutor.read_transcript": "Read transcript",
  "tutor.lesson_context":
    "This lesson, the slide you paused on, your lessons so far and your learner profile.",
  "tutor.you": "You",
  "tutor.tutor": "Tutor",
  "tutor.picker_prefix": "Tutor:",
  "tutor.picker_label": "Choose tutor",
  "tutor.picker_loading": "Loading tutors\u2026",
  "tutor.picker_error": "Could not load tutors",
  "tutor.picker_retry": "Retry tutors",
  "tutor.picker_done": "Done",
  "tutor.picker_next":
    "Your choice applies to your next question. Earlier replies keep their tutor.",
  "tutor.picker_only_guide": "Only the main guide is available.",
  "tutor.picker_unavailable": "Unavailable",
  "tutor.role_guide": "Main guide",
  "tutor.role_supporter": "Supporting tutor",
  "tutor.source_lesson": "From this Lesson's Cast",
  "tutor.source_course": "From the current Course Cast",
  "tutor.source_default": "Default Cast",
  "tutor.fallback_notice": "Using the default guide for this reply.",
  "tutor.interrupted_title": "Reply interrupted",
  "tutor.interrupted_body": "Only the words produced were charged.",
  "tutor.ask_again": "Ask again \u00b7 new estimate",
  "tutor.idle_body":
    "The tutor stopped responding. Only the words produced were charged.",
  "tutor.stopped_tag": "Stopped \u00b7 partial reply",
  "tutor.stopped_title": "Stopped",
  "tutor.stopped_body":
    "You stopped the reply. Only what was written is charged.",
  "tutor.failed_title": "Couldn't reply",
  "tutor.failed_body": "Nothing was charged. Your message is kept.",
  "tutor.retry": "Try again",
  "tutor.credits_title": "Not enough credits",
  "tutor.credits_body":
    "Your message is kept. Add credits or wait for your daily free credits.",
  "tutor.credits_next": "Next daily free credits: {time}.",
  "tutor.credits_link": "See credit options",
  "tutor.stale":
    "Your estimate expired, so it was refreshed. Review it and send again.",
  "tutor.approval_title": "This course needs a larger context",
  "tutor.approval_body":
    "Answering uses about {tokens} tokens of course context, above the standard {limit}. The credit price stays the same. Nothing is charged until you send.",
  "tutor.approval_action": "Approve larger context",
  "tutor.too_large_title": "This course is too large for one question",
  "tutor.too_large_body":
    "Nothing was charged and your message is kept. Ask about one lesson or unit, or shorten the question.",
  "tutor.offline_title": "Offline",
  "tutor.offline_body":
    "Tutor chat needs a connection. Your draft is saved on this device.",
  "tutor.load_error": "Could not load your chat.",
  "tutor.reload": "Reload",
  "tutor.loading": "Loading your chat\u2026",
  "tutor.delete": "Delete chat",
  "tutor.delete_title": "Delete this chat?",
  "tutor.delete_body": {
    one: "\u201c{title}\u201d and its {count} message will be removed. Quick notes you saved from it stay.",
    other:
      "\u201c{title}\u201d and its {count} messages will be removed. Quick notes you saved from it stay.",
  },
  "tutor.save": "Save as quick note",
  "tutor.saving": "Saving\u2026",
  "tutor.saved": "Saved to Quick notes",
  "tutor.saved_pending": "Saved to Quick notes \u00b7 Enrichment pending",
  "tutor.saved_open": "Open notes",
  "tutor.saved_truncated": "Shortened to fit a note.",
  "tutor.save_error": "Could not save the note. Try again.",
  "tutor.delete_cancel": "Cancel",
  "tutor.delete_confirm": "Delete",
  "tutor.delete_error": "Could not delete the chat. Try again.",
  "la.title": "Learning analytics",
  "la.region": "Learning analytics",
  "la.loading": "Loading learning analytics…",
  "la.timezone": "Time zone: {zone} · Local date: {date}",
  "la.expired_title": "Subscription expired",
  "la.expired_body": "Renew to use learning analytics and exports.",
  "la.renew": "Renew plan",
  "la.required_title": "Plus or Pro subscription required",
  "la.required_body":
    "Advanced learning analytics comes with Plus or Pro: course, daily XP, streak and completion breakdowns.",
  "la.view_plans": "View plans",
  "la.not_included_title": "Not included in your plan",
  "la.not_included_body":
    "Advanced learning analytics is part of Plus and Pro. Your learning history export still works.",
  "la.offline": "You are offline. Reconnect and retry.",
  "la.error": "Learning analytics unavailable. Retry.",
  "la.retry": "Retry",
  "la.total_xp": "Total XP",
  "la.completed": "Completed lessons",
  "la.perfect": "First-try perfect",
  "la.streak": "Current streak",
  "la.longest": "Longest: {count} days",
  "la.freezes": "Streak freezes",
  "la.used": "Used: {count}",
  "la.courses": "Course breakdown",
  "la.courses_empty": "No course completions recorded yet.",
  "la.col_course": "Course",
  "la.col_lessons": "Lessons",
  "la.col_xp": "Total XP",
  "la.col_perfect": "Perfect",
  "la.daily": "Daily XP activity",
  "la.daily_empty": "No daily activity recorded yet.",
  "la.col_date": "Date",
  "la.col_earned": "XP earned",
  "la.col_goal": "Goal status",
  "la.goal_met": "Goal met",
  "la.goal_open": "In progress",
  "la.xp": "{count} XP",
  "la.completion": "Completion breakdown",
  "la.avg_time": "Average lesson time",
  "la.total_time": "Total learning time",
  "la.first_done": "First completion",
  "la.last_done": "Latest completion",
  "la.minutes": "{count} min",
  "la.freeze_days": "Days protected by a freeze",
  "la.exports": "Data exports",
  "la.exports_note":
    "Exports are built from your records when you ask and are not stored on our servers.",
  "la.export_history": "Export learning history",
  "la.exporting": "Exporting…",
  "la.custom": "Custom analytics export",
  "la.custom_locked": "Custom export is part of Pro.",
  "la.export_custom": "Export custom analytics",
  "la.dimensions": "Dimensions to export",
  "la.dim_course": "Courses",
  "la.dim_daily": "Daily XP",
  "la.dim_streak": "Streaks",
  "la.dim_completions": "Completions",
  "la.dim_leagues": "Leagues",
  "la.pick_one": "Select at least one dimension to export.",
  "la.history_done": "Learning history export downloaded.",
  "la.custom_done": "Custom analytics export downloaded.",
  "la.history_failed": "Could not export learning history. Retry.",
  "la.custom_failed": "Could not export custom analytics. Retry.",
  "la.open": "Learning analytics and exports",
  "voice.offline": "Voice needs a connection. You can still type.",
  "voice.unavailable": "Voice is not available yet. You can still type.",
  "voice.unsupported": "This device cannot record audio. You can still type.",
  "voice.cap": "You have used today's voice limit. You can still type.",
  "voice.cap_resets":
    "You have used today's voice limit; it resets at {time}. You can still type.",
  "voice.denied_block":
    "Microphone access is off. Turn it on for Duolify in your device or browser settings, then try again. You can still type.",
  "voice.denied":
    "Microphone access is off. Turn it on for Duolify in your device or browser settings, then try again.",
  "voice.no_microphone": "No microphone was found. You can still type.",
  "voice.silent":
    "I could not hear anything. Try again closer to the microphone.",
  "voice.too_short": "Hold a little longer to record.",
  "voice.empty": "I could not make out any words.",
  "voice.rate_limited":
    "Too many recordings in a row. Wait a moment, then try again.",
  "voice.too_long":
    "That recording was too long to send. Keep it under 30 seconds.",
  "voice.unavailable_now":
    "Voice is not available right now. You can still type.",
  "voice.offline_retry":
    "Voice needs a connection. Try again when you are back online.",
  "voice.failed": "I could not turn that into text. You can retry or type.",
  "voice.stop_label": "Stop recording",
  "voice.recording_label": "Recording. Release to send, slide away to cancel",
  "voice.hold_label": "Hold to talk",
  "voice.transcribing": "Transcribing…",
  "voice.recording": "Recording",
  "voice.talk": "Talk",
  "voice.level": "Microphone level",
  "voice.hint_toggled":
    "Tap the microphone again to finish. Press Escape to cancel.",
  "voice.hint_hold": "Release to send. Slide away or press Escape to cancel.",
  "voice.hint_transcribing": "Turning your voice into text…",
  "voice.try_again": "Try again",
  "voice.type_instead": "Type instead",
  "voice.auto_send": "Send my question automatically after 2 seconds",
  "voice.explain_title": "Use the microphone",
  "voice.explain_body":
    "Dictation uses your microphone only while you hold the button. The recording is sent to a speech provider that does not keep it (zero data retention) and is turned into text. Duolify does not save the audio; only the text you send is kept.",
  "voice.allow": "Allow microphone",
  "voice.not_now": "Not now",
  "voice.countdown": "Sending in 2 s",
  "voice.cancel": "Cancel",
  "voice.cancelled": "Recording cancelled.",
  "voice.explain_short": "Mic is on only while you hold. Audio is not saved.",
  "imm.panel_label": "Answer about this area",
  "imm.hide": "Hide",
  "imm.saved": "Saved as Quick note",
  "imm.stop": "Stop",
  "imm.question": "Your question",
  "imm.ask_ph": "Ask about this",
  "imm.select_area": "Select area",
  "imm.ask": "Ask",
  "imm.note": "Quick note",
  "imm.voice_note": "Voice note",
  "imm.offline": "Offline. Your question is kept.",
  "imm.credits": "Not enough Credits.",
  "imm.failed": "No answer. Nothing charged. Retry.",
  "imm.block": "Too long for a quick answer.",
  "imm.answering": "Answering",
  "imm.ready": "Answer ready",
  "imm.swipe_close": "Swipe to close",
} satisfies Record<string, Message>;
export type MessageId = keyof typeof en;
// The Arabic catalogue is its own chunk: English readers never download it.
let arabic: Record<MessageId, Message> | undefined;
export const arabicLoaded = () => arabic !== undefined;
export async function loadArabic() {
  arabic ??= (await import("./messages.ar")).ar;
  Object.assign(arabic, lateArabic);
}
// Arabic strings registered by feature chunks before the Arabic catalogue itself has loaded.
const lateArabic: Record<string, Message> = {};
// Deliberately exposed only in the authored harness, never as a shipping language.
function expand(text: string) {
  return `［${text}${"～".repeat(Math.ceil(text.length * 0.4))}］`;
}
const expandMessage = (message: Message): Message =>
  typeof message === "string"
    ? expand(message)
    : (Object.fromEntries(
        Object.entries(message).map(([category, text]) => [
          category,
          expand(text),
        ]),
      ) as PluralMessage);
export const expanded = Object.fromEntries(
  Object.entries(en).map(([id, message]) => [id, expandMessage(message)]),
) as Record<MessageId, Message>;
/** Lazy copy: a feature chunk adds its own strings when it loads, keeping them out of the
 * first-load bundle. The ids are typed through `guidedEn`; only the values arrive late. */
export function registerMessages(
  enPart: Record<string, Message>,
  arPart: Record<string, Message>,
) {
  Object.assign(en, enPart);
  Object.assign(arabic ?? lateArabic, arPart);
  for (const [id, message] of Object.entries(enPart))
    (expanded as Record<string, Message>)[id] = expandMessage(message);
}
export type Locale = "en" | "ar" | "en-XA";
export function resolveLocale(value: unknown): Locale {
  if (value === "en-XA") return value;
  return typeof value === "string" && /^ar(?:-|$)/i.test(value) ? "ar" : "en";
}
export function createFormatter(value: unknown) {
  const locale = resolveLocale(value);
  const intlLocale = locale === "en-XA" ? "en" : locale;
  const number = (value: number) =>
    new Intl.NumberFormat(intlLocale).format(value);
  return {
    number,
    currency: (value: number, currency: string) =>
      new Intl.NumberFormat(intlLocale, { style: "currency", currency }).format(
        value,
      ),
    date: (
      value: Date,
      timeZone: string,
      options: Intl.DateTimeFormatOptions = { dateStyle: "medium" },
    ) =>
      new Intl.DateTimeFormat(intlLocale, {
        ...options,
        timeZone,
      }).format(value),
    t: (id: MessageId, values: Record<string, string | number> = {}) => {
      const catalog =
        locale === "ar" ? (arabic ?? en) : locale === "en-XA" ? expanded : en;
      const message: Message = catalog[id] ?? en[id];
      const sentence =
        typeof message === "string"
          ? message
          : (message[
              new Intl.PluralRules(intlLocale).select(Number(values.count ?? 0))
            ] ?? message.other);
      return sentence.replace(/\{(\w+)\}/g, (match, key: string) => {
        const value = values[key];
        // Unicode FSI/PDI isolates names in accessible labels and status sentences.
        return typeof value === "number"
          ? number(value)
          : typeof value === "string"
            ? locale === "ar" || /[\u0590-\u08ff]/.test(value)
              ? `\u2068${value}\u2069`
              : value
            : match;
      });
    },
  };
}
