import type { Step, Value } from "./types";
import { isSecure } from "./engine";

/** Closed set of routes a free-text turn can take. */
export type Route =
  "answer" | "question" | "faq" | "command" | "off_topic" | "fallback";
export type Command = "back" | "skip" | "help" | "restart";

export type Prefiltered =
  | { kind: "command"; command: Command }
  | { kind: "answer"; value: Value }
  | { kind: "none" };

/** Seam for the cheap intent classifier. This phase ships only the no-AI router below; the
 * classifier lands with `prompt-skill-routing-and-guardrails` and must never be called while
 * `intentRouterEnabled` is false. */
export interface IntentRouter {
  route(input: { scriptId: string; stepId: string; text: string }): Promise<{
    route: Route;
    faqId?: string;
    quoteRequired?: boolean;
  }>;
}
export const noAiRouter: IntentRouter = {
  route: () => Promise.resolve({ route: "fallback" }),
};
/** Flag `FEATURE_GUIDED_INTENT_ROUTER`: off until the guardrail change ships. */
export const intentRouterEnabled = false;

const commands: Record<string, Command> = {
  back: "back",
  "go back": "back",
  رجوع: "back",
  skip: "skip",
  تخطّي: "skip",
  تخطَّ: "skip",
  تخطي: "skip",
  help: "help",
  مساعدة: "help",
  "start over": "restart",
  restart: "restart",
  "ابدأ من جديد": "restart",
};

const yes = ["yes", "y", "yeah", "on", "نعم"];
const no = ["no", "n", "off", "لا"];

/** Reads text the way a form field would. Returns null when it cannot be a valid answer. */
export function parseAnswer(
  step: Step,
  raw: string,
  labelOf: (key: string) => string,
): Value | null {
  const text = raw.trim();
  if (!text) return null;
  const lower = text.toLowerCase();
  switch (step.widget) {
    case "time": {
      const m = /^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$/i.exec(text);
      if (!m) return null;
      let hour = Number(m[1]);
      const minute = Number(m[2] ?? 0);
      const meridiem = m[3]?.toLowerCase();
      if (meridiem) {
        if (hour < 1 || hour > 12) return null;
        hour = (hour % 12) + (meridiem === "pm" ? 12 : 0);
      }
      if (hour > 23 || minute > 59) return null;
      return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
    }
    case "number":
    case "rating":
      return /^\d{1,6}$/.test(text) ? Number(text) : null;
    case "date":
      return /^\d{4}-\d{2}-\d{2}$/.test(text) ? text : null;
    case "toggle":
      return yes.includes(lower) ? true : no.includes(lower) ? false : null;
    case "single":
    case "chips+text": {
      const hit = step.options?.find(
        (o) => o.id === lower || labelOf(o.labelKey).toLowerCase() === lower,
      );
      if (hit) return hit.id;
      return step.widget === "chips+text" ? text : null;
    }
    case "multi": {
      const ids: string[] = [];
      for (const part of text.split(/[,،]/)) {
        const p = part.trim().toLowerCase();
        const hit = step.options?.find(
          (o) => o.id === p || labelOf(o.labelKey).toLowerCase() === p,
        );
        if (!hit) return null;
        if (!ids.includes(hit.id)) ids.push(hit.id);
      }
      return ids;
    }
    case "text":
    case "longtext":
      return text;
    default:
      return null; // secure, file and confirm steps never take typed text
  }
}

/** Deterministic prefilter: commands first, then text that is a valid answer for this step.
 * Free text can never answer a secure step or confirm a destructive one. */
export function prefilter(
  step: Step,
  text: string,
  labelOf: (key: string) => string,
): Prefiltered {
  const key = text.trim().toLowerCase();
  const command = commands[key];
  if (command && !(command === "skip" && !step.skippable))
    return { kind: "command", command };
  if (isSecure(step) || step.widget === "confirm") return { kind: "none" };
  const value = parseAnswer(step, text, labelOf);
  return value === null ? { kind: "none" } : { kind: "answer", value };
}

/** At most 10 free-text turns a minute and 40 per script run. */
export function createLimiter(now: () => number = Date.now) {
  const turns: number[] = [];
  let total = 0;
  return {
    allow() {
      const cutoff = now() - 60_000;
      while (turns.length && turns[0] < cutoff) turns.shift();
      if (turns.length >= 10 || total >= 40) return false;
      turns.push(now());
      total += 1;
      return true;
    },
  };
}
