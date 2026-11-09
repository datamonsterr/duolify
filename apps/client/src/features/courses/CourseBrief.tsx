import { useEffect, useEffectEvent, useRef, useState } from "react";
import {
  GuidedChat,
  type ChatAnswer,
  type ChatMessage,
} from "../design/chat/GuidedChat";
import { Field } from "../design/chat/Feedback";
import { ResponsivePanel } from "../design/sheet/ResponsivePanel";
import { Button } from "../design/Button";
import { trackGuidedUi } from "../../lib/analytics";
import { profileApi } from "../accounts/learner-context";
import { session } from "../accounts/session";
import {
  briefApi,
  briefFields,
  emptyBrief,
  questions,
  seedBrief,
  type Brief,
  type BriefConfig,
  type BriefField,
  type BriefRevision,
  type BriefService,
} from "./brief";
import "./course-brief.css";
import { CoursePreview } from "./CoursePreview";

// Covers a stalled network as well as an assistant failure; API attempts finish sooner.
async function withDeadline<T>(request: Promise<T>): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      request,
      new Promise<never>((_, reject) => {
        timer = setTimeout(
          () => reject(new Error("Brief request timed out")),
          15000,
        );
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

type Draft = {
  brief: Brief;
  step: BriefField;
  followUps: BriefField[];
  messages: ChatMessage[];
  composer: string;
  form: boolean;
  revision: BriefRevision | null;
};
function initial(owner: string, brief: Brief): Draft {
  try {
    const raw = JSON.parse(
      sessionStorage.getItem(`course-brief:${owner}`) || "null",
    ) as Draft | null;
    if (
      raw &&
      briefFields.every((f) => typeof raw.brief?.[f.id] === "string") &&
      briefFields.some((f) => f.id === raw.step) &&
      Array.isArray(raw.followUps) &&
      Array.isArray(raw.messages)
    ) {
      if (
        raw.revision?.expires_at &&
        new Date(raw.revision.expires_at).getTime() <= Date.now()
      )
        raw.revision = null;
      return raw;
    }
  } catch {
    /* A disposable draft never prevents form access. */
  }
  return {
    brief,
    step: "topic",
    followUps: [],
    messages: [
      {
        id: "intro",
        author: "assistant",
        authorLabel: "Duolify",
        text: questions.topic,
        status: "complete",
      },
    ],
    composer: "",
    form: false,
    revision: null,
  };
}
export function CourseBrief({
  learnerId,
  service = briefApi,
  initialBrief = emptyBrief(),
}: {
  learnerId: string;
  service?: BriefService;
  initialBrief?: Brief;
}) {
  const [draft, setDraft] = useState(() => initial(learnerId, initialBrief));
  const [config, setConfig] = useState<BriefConfig | null>(null);
  const [pending, setPending] = useState(false);
  const [previewStep, setPreviewStepState] = useState(() => {
    try {
      const saved = sessionStorage.getItem(`course-creation-step:${learnerId}`);
      if (saved === "4") return 4;
    } catch {
      /* ignore */
    }
    return 1;
  });
  const setPreviewStep = (step: number) => {
    setPreviewStepState(step);
    try {
      sessionStorage.setItem(`course-creation-step:${learnerId}`, String(step));
    } catch {
      /* ignore */
    }
  };
  const [panelOpen, setPanelOpen] = useState(false);
  const [error, setError] = useState("");
  const [replies, setReplies] = useState<{ id: string; label: string }[]>([]);
  const operation = useRef(0);
  const invalidateOperation = useEffectEvent(() => {
    operation.current++;
  });
  const [announcedMessageIds] = useState(() => new Set<string>());
  useEffect(() => {
    let active = true;
    void service
      .config()
      .then((c) => {
        if (!active) return;
        setConfig(c);
        if (!c.enabled) setDraft((d) => ({ ...d, form: true }));
      })
      .catch(() => {
        if (active) {
          setConfig({ enabled: false, credits: null, pricing_version: "" });
          setDraft((d) => ({ ...d, form: true }));
        }
      });
    return () => {
      active = false;
      invalidateOperation();
    };
  }, [service]);
  useEffect(() => {
    try {
      sessionStorage.setItem(
        `course-brief:${learnerId}`,
        JSON.stringify(draft),
      );
    } catch {
      /* Current edits stay available in memory. */
    }
  }, [draft, learnerId]);
  function change(field: BriefField, value: string) {
    setPreviewStep(1);
    operation.current++;
    setPending(false);
    setError("");
    setDraft((d) => ({
      ...d,
      brief: { ...d.brief, [field]: value },
      revision: null,
    }));
  }
  async function answer(answer: ChatAnswer) {
    if (pending || !config?.enabled) return;
    const text = [
      ...answer.replyIds.map(
        (id) => replies.find((r) => r.id === id)?.label ?? id,
      ),
      answer.text.trim(),
    ]
      .filter(Boolean)
      .join("; ");
    const field = draft.step;
    const followUp = draft.followUps.includes(field);
    const value = answer.skipped
      ? ""
      : followUp && draft.brief[field]
        ? `${draft.brief[field]}; ${text}`
        : text;
    if (value.length > 500) {
      setError("Keep each brief answer within 500 characters.");
      return;
    }
    const updated = { ...draft.brief, [field]: value };
    const id = crypto.randomUUID();
    const messages: ChatMessage[] = [
      ...draft.messages.slice(-39),
      {
        id,
        author: "learner",
        authorLabel: "You",
        text: answer.skipped ? "Skipped" : text,
        status: "complete",
      },
    ];
    setDraft((d) => ({
      ...d,
      brief: updated,
      messages,
      composer: "",
      revision: null,
    }));
    setPending(true);
    setError("");
    const current = ++operation.current;
    try {
      const result = await withDeadline(
        service.chat({
          brief: draft.brief,
          current_step: field,
          answer: text,
          skipped: answer.skipped,
          is_follow_up: followUp,
        }),
      );
      if (current !== operation.current) return;
      if (
        !result.brief ||
        !briefFields.every((f) => typeof result.brief[f.id] === "string") ||
        ![...briefFields.map((f) => f.id), "confirm"].includes(result.next_step)
      )
        throw new Error("Invalid brief response");
      const next =
        result.next_step === "confirm"
          ? field
          : (result.next_step as BriefField);
      setReplies(result.quick_replies ?? []);
      setDraft((d) => ({
        ...d,
        brief: result.brief,
        step: next,
        followUps: result.is_follow_up
          ? [...new Set([...d.followUps, field])]
          : d.followUps,
        messages: [
          ...messages,
          {
            id: `${id}-assistant`,
            author: "assistant",
            authorLabel: "Duolify",
            text: result.message,
            status: "complete",
          },
        ],
      }));
    } catch {
      if (current === operation.current) {
        setError(
          "The assistant is unavailable. Your answers are kept in the form.",
        );
        setDraft((d) => ({ ...d, form: true }));
      }
    } finally {
      if (current === operation.current) setPending(false);
    }
  }
  async function confirm() {
    if (pending) return;
    if (!draft.brief.topic.trim()) {
      setError("Enter a topic before confirming your brief.");
      return;
    }
    const current = ++operation.current;
    setPending(true);
    setError("");
    trackGuidedUi({
      event: "ui_control_activated",
      surface: "course_brief",
      controlId: "confirm_brief",
    });
    try {
      const revision = await withDeadline(service.confirm(draft.brief));
      if (current === operation.current) setDraft((d) => ({ ...d, revision }));
    } catch {
      if (current === operation.current)
        setError(
          "Could not confirm. Your answers are kept; connect and retry.",
        );
    } finally {
      if (current === operation.current) setPending(false);
    }
  }
  function summary() {
    return (
      <form
        className="guided-summary"
        onSubmit={(e) => {
          e.preventDefault();
          void confirm();
        }}
      >
        <p>Every line is editable. Empty items are unanswered or skipped.</p>
        {briefFields.map((field) => (
          <Field
            key={field.id}
            label={field.label}
            value={draft.brief[field.id]}
            maxLength={500}
            hint={
              draft.brief[field.id]
                ? undefined
                : "Waiting for your answer · optional"
            }
            onChange={(e) => change(field.id, e.target.value)}
          />
        ))}
        {draft.revision && (
          <p role="status">
            Brief confirmed. Preview will require a separate quote and
            confirmation.
          </p>
        )}
        <Button type="submit" disabled={pending || !!draft.revision}>
          Confirm brief
        </Button>
        <Button
          variant="quiet"
          onClick={() => {
            setPanelOpen(false);
            setDraft((d) => ({ ...d, form: false }));
          }}
        >
          Keep chatting
        </Button>
      </form>
    );
  }
  return (
    <section
      className="course-brief guided-demo"
      aria-labelledby="create-course-title"
    >
      <header className="guided-demo-top">
        <a href="/courses">Cancel and keep draft</a>
        <h1 id="create-course-title">Create a course</h1>
        <ol className="brief-steps" aria-label="Creation steps">
          <li aria-current={previewStep === 1 ? "step" : undefined}>1 Brief</li>
          <li aria-current={previewStep === 2 ? "step" : undefined}>
            2 Preview
          </li>
          <li aria-current={previewStep === 3 ? "step" : undefined}>
            3 Outline
          </li>
          <li aria-current={previewStep === 4 ? "step" : undefined}>
            4 Create
          </li>
        </ol>
      </header>
      <p>
        AI uses your answers to shape your course. You can skip any question or
        use the form.
      </p>
      <p>
        {config?.enabled && config.credits !== null
          ? `Clarifying: ${config.credits} credits · platform paid.`
          : "Form entry is available while the assistant is disabled."}{" "}
        Nothing is charged until you confirm and request a separately quoted
        preview.
      </p>
      {error && <p role="alert">{error}</p>}
      {previewStep === 1 &&
        (draft.form ? (
          <div className="guided-fallback">
            <h2>Course brief</h2>
            {summary()}
          </div>
        ) : (
          <div className="guided-split">
            <GuidedChat
              messages={draft.messages}
              announcedMessageIds={announcedMessageIds}
              question={{
                id: `${draft.step}-${draft.followUps.length}`,
                replies,
                multiple:
                  draft.step === "include_topics" ||
                  draft.step === "skip_topics",
              }}
              draft={draft.composer}
              onDraftChange={(composer) =>
                setDraft((d) => ({ ...d, composer }))
              }
              onAnswer={(a) => void answer(a)}
              formHref="#brief-form"
              onFormFallback={(e) => {
                e.preventDefault();
                setDraft((d) => ({ ...d, form: true }));
              }}
              step={{
                current: briefFields.findIndex((f) => f.id === draft.step) + 1,
                total: briefFields.length,
              }}
              state={pending || !config ? "loading" : "ready"}
            />
            <ResponsivePanel
              title="Course brief"
              triggerLabel="Open course brief"
              sheetId="course_brief"
              open={panelOpen}
              onOpenChange={setPanelOpen}
            >
              {summary()}
            </ResponsivePanel>
          </div>
        ))}
      {draft.revision && (
        <CoursePreview
          key={draft.revision.id}
          revision={draft.revision}
          onStep={setPreviewStep}
          onBack={() => {
            setPreviewStep(1);
            setPanelOpen(false);
            setDraft((d) => ({ ...d, form: true }));
          }}
        />
      )}
    </section>
  );
}
export function CourseBriefPage() {
  const [seed, setSeed] = useState<Brief | null>(null);
  const [error, setError] = useState(false);
  useEffect(() => {
    let active = true;
    void profileApi
      .read()
      .then((profile) => {
        if (active) setSeed(seedBrief(profile));
      })
      .catch(() => {
        if (active) {
          setSeed(emptyBrief());
          setError(true);
        }
      });
    return () => {
      active = false;
    };
  }, []);
  if (!seed) return <p role="status">Loading your brief…</p>;
  return (
    <>
      {error && (
        <p role="status">Profile unavailable. Start with an empty brief.</p>
      )}
      <CourseBrief
        key={session.current()?.id}
        learnerId={session.current()?.id ?? ""}
        initialBrief={seed}
      />
    </>
  );
}
