import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Button } from "../design/Button";
import { Feedback } from "../design/chat/Feedback";
import { useUiPreferences } from "../design/localization/Preferences";
import type { MessageId } from "../design/localization/messages";
import {
  answer as answerStep,
  back,
  cancelEdit,
  current,
  edit,
  isComplete,
  restart,
  restore,
  skip as skipStep,
  stepById,
  toDraft,
  walk,
} from "./engine";
import type { Draft, GuidedState } from "./engine";
import { MascotAvatar } from "./MascotAvatar";
import { createLimiter, noAiRouter, prefilter } from "./routing";
import type { IntentRouter } from "./routing";
import type { Answer, GuidedScript, Step, Value } from "./types";
import { StepWidget } from "./widgets/StepWidget";
import "../design/chat/guided-chat.css";
import "./guided-script.css";

export type GuidedEvent =
  | { name: "started" | "resumed" | "completed" | "abandoned" }
  | {
      name: "step_answered" | "step_edited";
      stepId: string;
      widget: Step["widget"];
      skipped: boolean;
    }
  | { name: "free_text_asked"; stepId: string; route: string };

export type GuidedScriptChatProps = {
  script: GuidedScript;
  initialDraft?: Draft | null;
  /** Final submit to the existing endpoint. Rejects to show a retry. */
  onSubmit: (answers: Record<string, Value>, files: File[]) => Promise<void>;
  /** Called after every change with the draft to persist, or null when nothing is saveable. */
  onDraft?: (draft: Draft | null) => void;
  onEvent?: (event: GuidedEvent) => void;
  online?: boolean;
  router?: IntentRouter;
  /** The guide submits only when the step needs the network; offline keeps the answers. */
  needsNetwork?: boolean;
  onDone?: () => void;
  /** Destructive confirm card: Cancel leaves the flow without doing anything. */
  onCancel?: () => void;
};

const key = (k: string) => k as MessageId;

export function GuidedScriptChat({
  script,
  initialDraft,
  onSubmit,
  onDraft,
  onEvent,
  online = true,
  router = noAiRouter,
  needsNetwork = true,
  onDone,
  onCancel,
}: GuidedScriptChatProps) {
  const { t, reducedMotion } = useUiPreferences();
  const uid = useId();
  const [boot] = useState(() => restore(script, initialDraft ?? null));
  const [state, setState] = useState<GuidedState>(boot.state);
  const [notes, setNotes] = useState<{ id: string; text: string }[]>(() =>
    boot.notice
      ? [
          {
            id: "n0",
            text: t(
              boot.notice === "migrated"
                ? "guided.migrated"
                : "guided.restarted",
            ),
          },
        ]
      : [],
  );
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState<"idle" | "sending" | "failed" | "done">(
    "idle",
  );
  const [typed, setTyped] = useState("");
  const [fallback, setFallback] = useState<{
    text: string;
    stepId: string;
  } | null>(null);
  const [limited, setLimited] = useState(false);
  const limiter = useRef(createLimiter()).current;
  const files = useRef<Map<string, File>>(new Map());
  const log = useRef<HTMLDivElement>(null);
  const announced = useRef(new Set<string>());
  const widgetBox = useRef<HTMLDivElement>(null);
  const end = useRef<HTMLDivElement>(null);
  const mounted = useRef(false);
  const submitted = useRef(false);

  const now = current(script, state);
  const step = now ? stepById(script, now) : undefined;
  const { path } = useMemo(
    () => walk(script, state.answers),
    [script, state.answers],
  );
  const label = (k: string) => t(key(k));
  const promptId = (id: string) => `${uid}-q-${id}`;
  const complete = isComplete(script, state);
  const isLast = Boolean(
    step && script.steps[script.steps.indexOf(step) + 1]?.widget === "confirm",
  );

  const eventsRef = useRef(onEvent);
  useEffect(() => {
    eventsRef.current = onEvent;
  });
  useEffect(() => {
    eventsRef.current?.({ name: initialDraft ? "resumed" : "started" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Persist after each change. Secrets are filtered inside toDraft.
  useEffect(() => {
    if (status === "done") return;
    onDraft?.(toDraft(script, state));
  }, [script, state, status, onDraft]);

  // Polite log: each guide question is announced once per stable id.
  useEffect(() => {
    if (!step) return;
    const id = `${step.id}:${state.editing ? "edit" : "ask"}`;
    if (announced.current.has(id)) return;
    announced.current.add(id);
    const line = document.createElement("p");
    line.textContent = `${label("guided.guide")}: ${label(step.promptKey)}`;
    log.current?.append(line);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, state.editing]);

  // After each question focus moves to the first control; never auto-scroll under reduced motion.
  useEffect(() => {
    if (!mounted.current) {
      mounted.current = true;
      return;
    }
    const box = widgetBox.current;
    const target =
      box?.querySelector<HTMLElement>("[data-autofocus]") ??
      box?.querySelector<HTMLElement>("input, textarea, button");
    target?.focus({ preventScroll: reducedMotion });
    if (!reducedMotion) end.current?.scrollIntoView?.({ block: "nearest" });
  }, [now, state.editing, reducedMotion]);

  // Final submit once, after the confirm card is answered.
  useEffect(() => {
    if (!complete || submitted.current || status === "done") return;
    if (needsNetwork && !online) return;
    submitted.current = true;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setStatus("sending");
    const values: Record<string, Value> = {};
    for (const [id, a] of Object.entries(state.answers))
      if (!a.skipped) values[id] = a.value;
    onSubmit(values, [...files.current.values()]).then(
      () => {
        setStatus("done");
        eventsRef.current?.({ name: "completed" });
        onDone?.();
      },
      () => {
        submitted.current = false;
        setStatus("failed");
      },
    );
  }, [complete, online, needsNetwork, state.answers, onSubmit, onDone, status]);

  function changed(dropped: string[]) {
    if (!dropped.length) return;
    const names = dropped
      .map((id) => label(stepById(script, id)!.promptKey))
      .join(", ");
    setNotes((n) => [
      ...n,
      { id: `n${n.length + 1}`, text: t("guided.changed", { steps: names }) },
    ]);
  }

  function submitValue(value: Value, file?: File) {
    if (!step) return;
    const editing = state.editing === step.id;
    const out = answerStep(script, state, step.id, value);
    if (out.error) {
      setError(
        label(out.error === "required" ? "guided.required" : "guided.invalid"),
      );
      return;
    }
    if (file) files.current.set(step.id, file);
    setError(null);
    setState(out.state);
    changed(out.dropped);
    onEvent?.({
      name: editing ? "step_edited" : "step_answered",
      stepId: step.id,
      widget: step.widget,
      skipped: false,
    });
  }
  function doSkip() {
    if (!step) return;
    const out = skipStep(script, state, step.id);
    if (out.error) return;
    setError(null);
    setState(out.state);
    changed(out.dropped);
    onEvent?.({
      name: "step_answered",
      stepId: step.id,
      widget: step.widget,
      skipped: true,
    });
  }
  function run(command: "back" | "skip" | "help" | "restart") {
    setFallback(null);
    if (command === "back") setState(back(script, state));
    else if (command === "skip") doSkip();
    else if (command === "restart") {
      setState(restart(script));
      setNotes([]);
      submitted.current = false;
    } else
      setNotes((n) => [
        ...n,
        { id: `n${n.length + 1}`, text: t("guided.help") },
      ]);
  }

  async function sendTyped(event: FormEvent) {
    event.preventDefault();
    const text = typed.trim();
    if (!text || !step) return;
    if (!limiter.allow()) {
      setLimited(true);
      return;
    }
    setTyped("");
    const filtered = prefilter(step, text, label);
    if (filtered.kind === "command") {
      onEvent?.({ name: "free_text_asked", stepId: step.id, route: "command" });
      return run(filtered.command);
    }
    if (filtered.kind === "answer") {
      onEvent?.({ name: "free_text_asked", stepId: step.id, route: "answer" });
      return submitValue(filtered.value);
    }
    // No-AI path: the classifier seam stays closed in this phase, so offer buttons.
    const routed = await router.route({
      scriptId: script.id,
      stepId: step.id,
      text,
    });
    onEvent?.({
      name: "free_text_asked",
      stepId: step.id,
      route: routed.route,
    });
    setFallback({ text, stepId: step.id });
  }

  const display = (s: Step, a: Answer) => {
    if (a.skipped) return label("guided.skipped");
    const v = a.value;
    if (s.widget === "password") return "••••••••";
    if (s.widget === "toggle") return label(v ? "guided.yes" : "guided.no");
    if (s.widget === "rating") return t("guided.stars", { count: Number(v) });
    if (s.widget === "single" || s.widget === "chips+text") {
      const o = s.options?.find((x) => x.id === v);
      if (o) return label(o.labelKey);
    }
    if (Array.isArray(v))
      return v
        .map((id) => label(s.options?.find((o) => o.id === id)?.labelKey ?? id))
        .join(", ");
    return String(v);
  };

  const showComposer =
    step &&
    step.widget !== "confirm" &&
    step.widget !== "file" &&
    !["email", "code", "password"].includes(step.widget) &&
    !limited &&
    status !== "done";
  const summary =
    step?.widget === "confirm"
      ? (step.summaryOf ?? [])
          .filter((id) => state.answers[id])
          .map((id) => ({
            label: label(stepById(script, id)!.promptKey),
            answer: display(stepById(script, id)!, state.answers[id]),
          }))
      : undefined;

  return (
    <section
      className="guided-chat gc"
      aria-label={t("guided.title")}
      data-script={script.id}
    >
      <div
        className="gc-bar"
        aria-hidden="true"
        style={{
          inlineSize: `${Math.round((path.length / script.steps.length) * 100)}%`,
        }}
      />
      <div
        className="guided-transcript"
        role="region"
        aria-label={t("guided.transcript")}
        aria-live="off"
      >
        {notes.map((n) => (
          <div className="guided-message assistant" key={n.id}>
            <div className="gc-row">
              <MascotAvatar />
              <div className="guided-bubble" dir="auto">
                {n.text}
              </div>
            </div>
          </div>
        ))}
        {path
          .filter((id) => id !== state.editing)
          .map((id) => {
            const s = stepById(script, id)!;
            const a = state.answers[id];
            if (s.widget === "confirm") return null;
            return (
              <div key={id} className="gc-pair">
                <div className="guided-message assistant">
                  <div className="gc-row">
                    <MascotAvatar />
                    <div className="guided-bubble" dir="auto">
                      {label(s.promptKey)}
                    </div>
                  </div>
                </div>
                <div className="guided-message learner">
                  <button
                    type="button"
                    className="guided-bubble gc-answer"
                    aria-label={t("guided.edit", { answer: display(s, a) })}
                    onClick={() => {
                      setError(null);
                      setState(edit(state, id));
                    }}
                    disabled={status === "sending" || status === "done"}
                  >
                    {display(s, a)}
                  </button>
                </div>
              </div>
            );
          })}
        {step && (
          <div className="guided-message assistant">
            <div className="gc-row">
              <MascotAvatar />
              <div className="guided-bubble" dir="auto" id={promptId(step.id)}>
                {label(step.promptKey)}
                {isLast && step.widget !== "confirm"
                  ? ` ${label("guided.last")}`
                  : ""}
              </div>
            </div>
          </div>
        )}
        {complete && status !== "done" && (
          <p className="field-hint">{label("guided.done")}</p>
        )}
        {status === "done" && (
          <div className="guided-message assistant">
            <div className="gc-row">
              <MascotAvatar />
              <div className="guided-bubble" role="status">
                {label("guided.submitted")}
              </div>
            </div>
          </div>
        )}
        <div ref={end} />
      </div>
      <div
        ref={log}
        className="guided-sr-only"
        role="log"
        aria-label={t("guided.log")}
        aria-live="polite"
        aria-relevant="additions"
      />
      {!online && needsNetwork && (
        <Feedback tone="warning" title={t("guided.offline")}>
          {t("guided.offline_keep")}
        </Feedback>
      )}
      {status === "failed" && (
        <Feedback
          tone="danger"
          title={t("guided.submit_failed")}
          action={t("guided.retry")}
          onAction={() => setStatus("idle")}
        />
      )}
      {step && status !== "done" && (
        <div ref={widgetBox} className="gc-slot">
          <StepWidget
            key={`${step.id}:${state.editing ?? ""}:${Object.keys(state.answers).length}`}
            step={step}
            promptId={promptId(step.id)}
            initial={state.answers[step.id]?.value ?? undefined}
            error={error}
            editing={state.editing === step.id}
            summary={summary}
            disabled={status === "sending"}
            onSubmit={submitValue}
            onSkip={doSkip}
            onCancel={
              step.destructive
                ? (onCancel ?? (() => setState(back(script, state))))
                : state.editing === step.id
                  ? () => setState(cancelEdit(state))
                  : undefined
            }
          />
        </div>
      )}
      {fallback && step && fallback.stepId === step.id && (
        <div
          className="gc-fallback"
          role="group"
          aria-label={t("guided.not_clear")}
        >
          <p>{t("guided.not_clear")}</p>
          <div className="gc-actions">
            <Button
              variant="secondary"
              onClick={() => {
                const parsed = prefilter(step, fallback.text, label);
                if (parsed.kind === "answer") submitValue(parsed.value);
                else submitValue(fallback.text);
                setFallback(null);
              }}
            >
              {t("guided.use_answer")}
            </Button>
            {router !== noAiRouter && (
              <Button variant="secondary" onClick={() => setFallback(null)}>
                {t("guided.ask_guide")}
              </Button>
            )}
            {step.skippable && (
              <Button
                variant="quiet"
                onClick={() => {
                  setFallback(null);
                  doSkip();
                }}
              >
                {t("guided.skip")}
              </Button>
            )}
          </div>
        </div>
      )}
      {limited && <p className="field-hint">{t("guided.limit")}</p>}
      {showComposer && (
        <form className="guided-composer gc-composer" onSubmit={sendTyped}>
          <label className="guided-sr-only" htmlFor={`${uid}-free`}>
            {t("guided.type_instead")}
          </label>
          <div className="gc-composer-row">
            <input
              id={`${uid}-free`}
              type="text"
              dir="auto"
              autoComplete="off"
              placeholder={t("guided.type_instead")}
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
            />
            <Button type="submit" variant="secondary" disabled={!typed.trim()}>
              {t("guided.send")}
            </Button>
          </div>
        </form>
      )}
    </section>
  );
}
