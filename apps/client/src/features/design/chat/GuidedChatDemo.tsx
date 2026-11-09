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
            ? { ...line, error: "Enter a topic before confirming." }
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
        fixtureState === "confirm-error"
          ? "Could not confirm. Your draft is kept; try again."
          : undefined
      }
    />
  );
  return (
    <main className="guided-demo">
      <header className="guided-demo-top">
        <div>
          <span className="eyebrow">COMPONENT FIXTURES · E13-03</span>
          <h1>Let's shape your course</h1>
          <p>Brief → Preview → Outline → Create</p>
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
        <Badge role="info">Brief · 1 question left</Badge>
        <p>You can skip any question. Nothing starts automatically.</p>
      </div>
      {form ? (
        <section id="brief-form" className="guided-fallback">
          <h2 ref={formHeading} tabIndex={-1}>
            Course brief form
          </h2>
          <a
            href="#chat"
            className="guided-link"
            onClick={(event) => {
              event.preventDefault();
              setForm(false);
            }}
          >
            Back to chat
          </a>
          <fieldset>
            <legend>Starting level</legend>
            {["beginner", "intermediate", "advanced"].map((value) => (
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
                {value[0].toUpperCase() + value.slice(1)}
              </label>
            ))}
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
            Send learning reminders
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
            question={question}
            draft={draft}
            onDraftChange={setDraft}
            onAnswer={(answer) => {
              const text = answer.skipped
                ? "Skipped"
                : [
                    answer.text.trim(),
                    ...answer.replyIds.map(
                      (id) =>
                        question.replies.find((reply) => reply.id === id)
                          ?.label,
                    ),
                  ]
                    .filter(Boolean)
                    .join(" · ");
              setMessages([
                ...messages,
                {
                  id: crypto.randomUUID(),
                  author: "learner",
                  authorLabel: "You",
                  text,
                  status: "complete",
                },
                {
                  id: crypto.randomUUID(),
                  author: "assistant",
                  authorLabel: "Duolify",
                  text: "Thanks. Review your brief and confirm when you're ready.",
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
            title="Course brief"
            triggerLabel="Open course brief"
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
          title="Outline chat"
          triggerLabel="Open outline chat"
          sheetId="outline_chat"
        >
          <div className="guided-message learner">
            <strong>You</strong>
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
                    ? "Proposal applied. Undo available."
                    : "Proposal discarded. Outline unchanged."
                }
              />
              <Button
                variant="quiet"
                onClick={() => setProposalStatus("ready")}
              >
                Show suggestion
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
        <h2>Sheet keyboard example</h2>
        <p>{paused ? "Player paused" : "Player ready"}</p>
        <Button variant="secondary" onClick={() => setSheetOpen(true)}>
          Open keyboard sheet
        </Button>
        <Sheet
          open={sheetOpen}
          onClose={() => setSheetOpen(false)}
          title="Keyboard sheet"
          sheetId="fixture_sheet"
          onPausePlayer={pausePlayer}
        >
          <p>Opening a sheet pauses the player and keeps the slide position.</p>
          <label className="guided-field">
            Your note
            <textarea rows={3} />
          </label>
          <Button variant="quiet">Last action</Button>
        </Sheet>
      </section>
    </main>
  );
}
