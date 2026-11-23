// Guided chat scripts are data: typed, versioned, bundled in the client, never evaluated.
export type Value = string | number | boolean | string[] | null;

/** Closed expression language for branches. No code evaluation. */
export type Expr =
  | { eq: [string, Value] }
  | { in: [string, Value[]] }
  | { answered: string }
  | { not: Expr };

export type WidgetKind =
  | "single"
  | "multi"
  | "chips+text"
  | "time"
  | "date"
  | "number"
  | "toggle"
  | "text"
  | "longtext"
  | "file"
  | "rating"
  | "confirm"
  | "email"
  | "code"
  | "password";

/** Widgets whose value is a secret or credential: never drafted, never routed from free text. */
export const secureWidgets: readonly WidgetKind[] = [
  "email",
  "code",
  "password",
];

export type Validate =
  | { type: "string"; min?: number; max: number }
  | { type: "hhmm" }
  | { type: "date"; min?: string; max?: string }
  | { type: "number"; min: number; max: number; integer?: boolean }
  | { type: "enum" }
  | { type: "multi"; maxItems: number }
  | { type: "email" }
  | { type: "code"; length: number }
  | { type: "boolean" }
  | { type: "rating"; max: number }
  | { type: "file"; maxBytes: number; accept: string[] };

export type Option = { id: string; labelKey: string };

export type Step = {
  id: string;
  widget: WidgetKind;
  /** Message id for the guide's question (EN and AR). */
  promptKey: string;
  options?: Option[];
  validate?: Validate;
  /** Names an entry of `apiLimits` the validator must not be looser than. */
  limit?: string;
  skippable?: boolean;
  /** false for secrets: never saved to the draft or local copy. */
  persist: boolean;
  when?: Expr;
  /** First matching rule wins. Targets must come later in the script. */
  next?: { when: Expr; to: string }[];
  analyticsId?: string;
  /** Confirm step: step ids summarised on the card. */
  summaryOf?: string[];
  /** Confirm step: names the existing endpoint the script submits to. */
  submit?: string;
  destructive?: boolean;
  /** Secure inputs: native attributes so autofill and password managers work. */
  input?: { name: string; autocomplete: string; inputMode?: string };
  /** Message ids shown for validator errors, by validator name. */
  errorKey?: string;
};

export type GuidedScript = {
  id: string;
  version: number;
  entryPoints: string[];
  /** Per-script rollout flag; the old form stays one release behind it. */
  flag: string;
  steps: Step[];
};

export type Answer = { value: Value; skipped?: boolean };
export type Answers = Record<string, Answer>;
