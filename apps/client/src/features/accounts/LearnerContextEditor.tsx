import { useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { Feedback } from "../design/chat/Feedback";
import { trackGuidedUi } from "../../lib/analytics";
import {
  contextFields,
  contextValues,
  validateContext,
  type ContextFields,
  type LearnerContext,
} from "./learner-context";
import "./learner-context.css";

// E1-11 hands its controlled answers here. Every field uses the same draft in form and summary.
// Drafts stay in memory and are never sent to analytics or persisted before confirmation.
export function LearnerContextEditor({
  profile,
  setup = false,
  initialAnswers = {},
  onCommit,
  onExport,
}: {
  profile: LearnerContext;
  setup?: boolean;
  initialAnswers?: Partial<ContextFields>;
  onCommit: (
    action: "save" | "skip",
    fields: Partial<ContextFields>,
  ) => Promise<LearnerContext>;
  onExport?: () => Promise<void>;
}) {
  const [draft, setDraft] = useState<ContextFields>(() => ({
    ...contextValues(profile),
    ...initialAnswers,
  }));
  const [step, setStep] = useState<"intro" | "form" | "summary">(
    setup ? "intro" : "form",
  );
  const [errors, setErrors] = useState<
    Partial<Record<keyof ContextFields, string>>
  >({});
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [saved, setSaved] = useState(false);
  const [dirty, setDirty] = useState(Object.keys(initialAnswers).length > 0);
  const [online, setOnline] = useState(navigator.onLine);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
  }, []);
  async function commit(action: "save" | "skip") {
    if (pending || !online) return;
    const invalid =
      action === "save" ? validateContext(draft, profile.text_limit) : {};
    setErrors(invalid);
    if (Object.keys(invalid).length) {
      setError("Check the highlighted fields.");
      document.getElementById(`context-${Object.keys(invalid)[0]}`)?.focus();
      return;
    }
    setPending(true);
    setError("");
    try {
      const result = await onCommit(action, action === "skip" ? {} : draft);
      setDraft(contextValues(result));
      setSaved(true);
      setDirty(false);
      heading.current?.focus();
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Could not save profile. Please retry.",
      );
    } finally {
      setPending(false);
    }
  }
  const change = (
    id: keyof ContextFields,
    value: string | number | boolean | null,
  ) => {
    setDraft((current) => ({ ...current, [id]: value }));
    setDirty(true);
    setSaved(false);
    setErrors((current) => ({ ...current, [id]: undefined }));
  };
  if (setup && saved)
    return (
      <div className="learner-context">
        <h1 ref={heading} tabIndex={-1}>
          Setup complete
        </h1>
        <p>You can change or clear these answers at any time.</p>
        <a className="shell-link" href="/profile">
          Edit profile
        </a>
        <a className="shell-link" href="/">
          Start learning
        </a>
      </div>
    );
  return (
    <section className="learner-context" aria-busy={pending}>
      {setup ? (
        <h1 ref={heading} tabIndex={-1}>
          {step === "summary"
            ? "Confirm your learner context"
            : "A little about you"}
        </h1>
      ) : (
        <h2 ref={heading} tabIndex={-1}>
          Learner context
        </h2>
      )}
      <aside className="context-disclosure">
        <strong>AI uses your optional answers</strong>
        <p>
          Your goal, topic, background and level help our configured AI
          providers tailor examples. Only share what you want. Profile text is
          never sent to analytics. Every answer is optional.
        </p>
      </aside>
      {!online && (
        <Feedback
          tone="warning"
          title="Offline. Your draft is kept here. Connect to save or skip."
        />
      )}
      {step === "intro" ? (
        <div className="context-welcome">
          <p>
            Use the setup form to share your goals, or skip and start learning.
            Collected setup answers stay a draft until you confirm.
          </p>
          <Button
            onClick={() => {
              trackGuidedUi({
                event: "ui_control_activated",
                surface: "guided_chat",
                controlId: "form_fallback",
              });
              setStep("form");
            }}
          >
            Use form instead
          </Button>
          <Button
            variant="secondary"
            disabled={!online}
            pending={pending}
            onClick={() => void commit("skip")}
          >
            Skip setup
          </Button>
          {error && <Feedback tone="danger" title={error} blocking />}
        </div>
      ) : (
        <form
          noValidate
          onSubmit={(event) => {
            event.preventDefault();
            if (setup && step === "form") {
              setStep("summary");
              heading.current?.focus();
            } else void commit("save");
          }}
        >
          <div className="context-form-grid">
            {contextFields.map((field) => {
              const id = `context-${field.id}`;
              const value = draft[field.id];
              const props = {
                id,
                disabled: pending,
                "aria-invalid": errors[field.id] ? (true as const) : undefined,
                "aria-describedby": `${id}-help`,
              };
              return (
                <div className="context-field" key={field.id}>
                  <label htmlFor={id}>{field.label}</label>
                  {field.kind === "text" ? (
                    <textarea
                      {...props}
                      rows={3}
                      value={String(value)}
                      onChange={(event) => change(field.id, event.target.value)}
                    />
                  ) : field.kind === "level" ? (
                    <select
                      {...props}
                      value={String(value)}
                      onChange={(event) => change(field.id, event.target.value)}
                    >
                      <option value="">Not shared</option>
                      <option value="beginner">Beginner</option>
                      <option value="intermediate">Intermediate</option>
                      <option value="advanced">Advanced</option>
                      <option value="expert">Expert</option>
                    </select>
                  ) : field.kind === "reminder" ? (
                    <select
                      {...props}
                      value={value === null ? "" : String(value)}
                      onChange={(event) =>
                        change(
                          field.id,
                          event.target.value === ""
                            ? null
                            : event.target.value === "true",
                        )
                      }
                    >
                      <option value="">Not shared</option>
                      <option value="true">Enabled</option>
                      <option value="false">Disabled</option>
                    </select>
                  ) : (
                    <input
                      {...props}
                      type={
                        field.kind === "number"
                          ? "number"
                          : field.kind === "time"
                            ? "time"
                            : "text"
                      }
                      {...(field.kind === "number"
                        ? { min: 1, max: field.max, step: 1 }
                        : {})}
                      value={value === null ? "" : String(value)}
                      onChange={(event) =>
                        change(
                          field.id,
                          field.kind === "number"
                            ? event.target.value === ""
                              ? null
                              : Number(event.target.value)
                            : event.target.value,
                        )
                      }
                    />
                  )}
                  <span
                    id={`${id}-help`}
                    className={errors[field.id] ? "field-error" : "field-hint"}
                  >
                    {errors[field.id] ||
                      (field.kind === "text"
                        ? `${Array.from(String(value)).length} / ${profile.text_limit} characters`
                        : field.kind === "zone"
                          ? "IANA name, for example Asia/Ho_Chi_Minh. Leave blank to clear."
                          : field.kind === "time"
                            ? "Local time in your selected time zone. Leave blank to skip."
                            : "Optional. Leave blank to skip or clear.")}
                  </span>
                </div>
              );
            })}
          </div>
          <Feedback
            title={
              saved
                ? "Profile saved"
                : dirty
                  ? "Unsaved changes"
                  : "Review your optional answers"
            }
            tone={saved ? "success" : "info"}
          />
          {error && <Feedback tone="danger" title={error} blocking />}
          {setup && (
            <p>
              Nothing is saved until you select Confirm profile. Skip setup
              discards these draft answers.
            </p>
          )}
          <div className="context-actions">
            <Button type="submit" disabled={!online} pending={pending}>
              {error
                ? "Retry save"
                : setup
                  ? step === "form"
                    ? "Review summary"
                    : "Confirm profile"
                  : "Save"}
            </Button>
            {setup && (
              <Button
                variant="secondary"
                disabled={pending || !online}
                onClick={() => void commit("skip")}
              >
                Skip setup
              </Button>
            )}
            {setup && step === "summary" && (
              <Button
                variant="quiet"
                disabled={pending}
                onClick={() => setStep("form")}
              >
                Use form instead
              </Button>
            )}
            <Button
              variant="secondary"
              disabled={pending}
              onClick={() => {
                setDraft({
                  ...contextValues(profile),
                  goal: "",
                  topic: "",
                  background: "",
                  level: "",
                  daily_minutes: null,
                  daily_xp: null,
                  reminder_enabled: null,
                  reminder_time: "",
                });
                setDirty(true);
                setSaved(false);
                setError("");
                setErrors({});
              }}
            >
              Clear profile
            </Button>
            {onExport && (
              <Button
                variant="secondary"
                disabled={pending || !online}
                onClick={() => {
                  setError("");
                  void onExport().catch(() =>
                    setError("Could not export profile. Please retry."),
                  );
                }}
              >
                Export profile data
              </Button>
            )}
          </div>
        </form>
      )}
    </section>
  );
}
