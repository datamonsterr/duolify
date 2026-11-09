import { useUiPreferences } from "../localization/Preferences";
import { PreferenceControls } from "../localization/PreferenceControls";
import { useCallback, useRef, useState, useSyncExternalStore } from "react";
import { Button } from "../Button";
import { Badge } from "../Badge";
import { ResponsivePanel } from "../sheet/ResponsivePanel";
import { Sheet } from "../sheet/Sheet";
import { GuidedChat, type ChatMessage } from "./GuidedChat";
import { LiveSummary } from "./LiveSummary";
import { OutlineEditor, type Outline } from "./OutlineEditor";
import { ProposalDiff } from "./ProposalDiff";
import { Feedback } from "./Feedback";
import {
  applyFixtureProposal,
  fixtureProposal,
  initialOutline,
  initialSummary,
} from "./fixtures";
import "./guided-chat.css";

function subscribeOnline(notify: () => void) {
  window.addEventListener("online", notify);
  window.addEventListener("offline", notify);
  return () => {
    window.removeEventListener("online", notify);
    window.removeEventListener("offline", notify);
  };
}
const question = {
  id: "viewing",
  multiple: true,
  replies: [
    { id: "city", label: "City balcony" },
    { id: "both", label: "Both" },
    { id: "dark", label: "Dark sky is fine" },
    { id: "unsure", label: "Not sure" },
  ],
};
const start: ChatMessage = {
  id: "start",
  author: "assistant",
  authorLabel: "Duolify",
  text: "From your setup: astronomy, for fun, to explain the night sky to your kids. Should examples be things you can see from a city balcony, or is a dark-sky trip OK?",
  status: "complete",
};

// Authored demo only. Feature consumers own AI, saving, prices and authoritative outcomes.
export function GuidedChatDemo() {
  const { t } = useUiPreferences();
  const localizedQuestion = {
    ...question,
    replies: question.replies.map((reply, index) => ({
      ...reply,
      label: t(
        (["demo.city", "demo.both", "demo.dark_sky", "demo.unsure"] as const)[
          index
        ],
      ),
    })),
  };
  const [lines, setLines] = useState(initialSummary);
  const [draft, setDraft] = useState("");
  const [messages, setMessages] = useState<ChatMessage[]>([start]);
  const [form, setForm] = useState(false);
  const [fixtureState, setFixtureState] = useState("ready");
  const [unsaved, setUnsaved] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const [level, setLevel] = useState("beginner");
  const [reminders, setReminders] = useState(false);
  const [outline, setOutline] = useState(initialOutline);
  const [proposalValue, setProposalValue] = useState<Outline | null>(null);
  const [proposalStatus, setProposalStatus] = useState<
    "ready" | "applied" | "discarded"
  >("ready");
  const [sheetOpen, setSheetOpen] = useState(false);
  const [paused, setPaused] = useState(false);
  const pausePlayer = useCallback(() => setPaused(true), []);
  const formHeading = useRef<HTMLHeadingElement>(null);
  const online = useSyncExternalStore(
    subscribeOnline,
    () => navigator.onLine,
    () => true,
  );
  function edit(id: string, value: string) {
    setLines((previous) =>
      previous.map((line) =>
        line.id === id ? { ...line, value, error: undefined } : line,
      ),
    );
    setUnsaved(true);
    setConfirmed(false);
  }
  function confirm() {
    if (!lines.find((line) => line.id === "topic")?.value.trim()) {
      setLines(
        lines.map((line) =>
          line.id === "topic"
            ? { ...line, error: t("demo.topic_error") }
            : line,
        ),
      );
      return;
    }
    if (fixtureState === "confirm-error") return;
    setConfirmed(true);
    setUnsaved(false);
  }
  const summary = (
    <LiveSummary
      lines={lines}
      onChange={edit}
      onConfirm={confirm}
      unsaved={unsaved}
      confirmed={confirmed}
      pending={fixtureState === "pending"}
      error={
        fixtureState === "confirm-error" ? t("demo.confirm_error") : undefined
      }
    />
  );
  return (
    <main className="guided-demo">
      <PreferenceControls testLocales />
      <header className="guided-demo-top">
        <div>
          <span className="eyebrow">COMPONENT FIXTURES · E13-03</span>
          <h1>{t("demo.title")}</h1>
          <p>{t("demo.path")}</p>
        </div>
        <label className="guided-field">
          Fixture state
          <select
            value={fixtureState}
            onChange={(event) => setFixtureState(event.target.value)}
          >
            <option value="ready">Ready</option>
            <option value="loading">Loading reply</option>
            <option value="empty">Empty thread</option>
            <option value="error">Chat error</option>
            <option value="long">Long content</option>
            <option value="pending">Confirm pending</option>
            <option value="confirm-error">Confirm error</option>
          </select>
        </label>
      </header>
      <div className="guided-step-label">
        <Badge role="info">{t("demo.remaining")}</Badge>
        <p>{t("demo.skip_hint")}</p>
      </div>
      {form ? (
        <section id="brief-form" className="guided-fallback">
          <h2 ref={formHeading} tabIndex={-1}>
            {t("demo.form")}
          </h2>
          <a
            href="#chat"
            className="guided-link"
            onClick={(event) => {
              event.preventDefault();
              setForm(false);
            }}
          >
            {t("demo.back")}
          </a>
          <fieldset>
            <legend>{t("demo.level")}</legend>
            {(["beginner", "intermediate", "advanced"] as const).map(
              (value) => (
                <label className="guided-choice" key={value}>
                  <input
                    type="radio"
                    name="level"
                    checked={level === value}
                    onChange={() => {
                      setLevel(value);
                      setUnsaved(true);
                      setConfirmed(false);
                    }}
                  />
                  {t(`demo.${value}`)}
                </label>
              ),
            )}
          </fieldset>
          <label className="guided-choice">
            <input
              type="checkbox"
              checked={reminders}
              onChange={(event) => {
                setReminders(event.target.checked);
                setUnsaved(true);
                setConfirmed(false);
              }}
            />
            {t("demo.reminders")}
          </label>
          {summary}
        </section>
      ) : (
        <div className="guided-split" id="chat">
          <GuidedChat
            messages={
              fixtureState === "empty"
                ? []
                : fixtureState === "long"
                  ? [
                      {
                        ...start,
                        text: "A long authored question 日本語 العربية Tiếng Việt ".repeat(
                          30,
                        ),
                      },
                    ]
                  : messages
            }
            question={localizedQuestion}
            draft={draft}
            onDraftChange={setDraft}
            onAnswer={(answer) => {
              const text = answer.skipped
                ? t("demo.skipped")
                : [
                    answer.text.trim(),
                    ...answer.replyIds.map(
                      (id) =>
                        localizedQuestion.replies.find(
                          (reply) => reply.id === id,
                        )?.label,
                    ),
                  ]
                    .filter(Boolean)
                    .join(" · ");
              setMessages([
                ...messages,
                {
                  id: crypto.randomUUID(),
                  author: "learner",
                  authorLabel: t("demo.you"),
                  text,
                  status: "complete",
                },
                {
                  id: crypto.randomUUID(),
                  author: "assistant",
                  authorLabel: "Duolify",
                  text: t("demo.thanks"),
                  status: "complete",
                },
              ]);
              edit("viewing", answer.skipped ? "" : text);
              setDraft("");
            }}
            formHref="#brief-form"
            onFormFallback={(event) => {
              event.preventDefault();
              setForm(true);
              queueMicrotask(() => formHeading.current?.focus());
            }}
            step={{ current: 3, total: 4 }}
            state={
              !online
                ? "offline"
                : fixtureState === "loading"
                  ? "loading"
                  : fixtureState === "error"
                    ? "error"
                    : "ready"
            }
            onRetry={() => setFixtureState("ready")}
          />
          <ResponsivePanel
            title={t("demo.brief")}
            triggerLabel={t("demo.open_brief")}
            sheetId="course_brief"
          >
            {summary}
          </ResponsivePanel>
        </div>
      )}
      <div className="guided-split outline-composition">
        <OutlineEditor
          initialValue={initialOutline}
          onChange={setOutline}
          proposal={proposalValue}
        />
        <ResponsivePanel
          title={t("demo.outline_chat")}
          triggerLabel={t("demo.open_outline")}
          sheetId="outline_chat"
        >
          <div className="guided-message learner">
            <strong>{t("demo.you")}</strong>
            <div className="guided-bubble">
              Add a short unit on choosing a first telescope at the end.
            </div>
          </div>
          {proposalStatus === "ready" ? (
            <ProposalDiff
              proposal={fixtureProposal}
              onApply={() => {
                const next = applyFixtureProposal(outline);
                setProposalValue(next);
                setOutline(next);
                setProposalStatus("applied");
              }}
              onDiscard={() => setProposalStatus("discarded")}
            />
          ) : (
            <>
              <Feedback
                tone={proposalStatus === "applied" ? "success" : "info"}
                title={
                  proposalStatus === "applied"
                    ? t("demo.applied")
                    : t("demo.discarded")
                }
              />
              <Button
                variant="quiet"
                onClick={() => setProposalStatus("ready")}
              >
                {t("demo.show")}
              </Button>
            </>
          )}
          <p>
            Authored suggestion. This fixture makes no AI calls and spends no
            credits.
          </p>
        </ResponsivePanel>
      </div>
      <section className="guided-sheet-host" aria-label="Player host fixture">
        <h2>{t("demo.keyboard")}</h2>
        <p>{paused ? t("demo.paused") : t("demo.ready")}</p>
        <Button variant="secondary" onClick={() => setSheetOpen(true)}>
          {t("demo.open_keyboard")}
        </Button>
        <Sheet
          open={sheetOpen}
          onClose={() => setSheetOpen(false)}
          title={t("demo.sheet")}
          sheetId="fixture_sheet"
          onPausePlayer={pausePlayer}
        >
          <p>{t("demo.sheet_hint")}</p>
          <label className="guided-field">
            {t("demo.note")}
            <textarea rows={3} />
          </label>
          <Button variant="quiet">{t("demo.last")}</Button>
        </Sheet>
      </section>
    </main>
  );
}
