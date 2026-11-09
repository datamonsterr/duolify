import { useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { session } from "../accounts/session";
import { quoteAPI, type ActionQuote } from "../pricing/quotes";
import { sendCommand } from "../lesson-player/bridge";
import { briefFields, type BriefRevision } from "./brief";
import { useUiPreferences } from "../design/localization/Preferences";
import { CourseCreate } from "./CourseCreate";
import type { ConfirmedOutline } from "./outline";
import { CourseOutline } from "./CourseOutline";
import {
  outlineApi,
  readWorkingOutline,
  sampleOutline,
  type OutlineDraft,
} from "./outline";
import type { Outline } from "../design/chat/OutlineEditor";

type PreviewQuote = Pick<
  ActionQuote,
  "id" | "content_ceiling" | "pricing_version" | "expires_at"
>;
export type Preview = {
  sample_outline: {
    title: string;
    units: { title: string; lessons: { title: string; goal: string }[] }[];
  };
  example_slides: {
    slide_id: string;
    html: string;
    narration: string;
    duration_ms: number;
  }[];
};
interface PreviewService {
  wallet?(): Promise<{
    purchased: number;
    subscription: number;
    unverified?: boolean;
  }>;
  quote(revision: string): Promise<PreviewQuote>;
  accept(id: string): Promise<unknown>;
  preview(revision: string, quoteId: string): Promise<Preview>;
}
const previewApi: PreviewService = {
  wallet: () => session.request("/v1/me/credits"),
  quote: (revision) => quoteAPI.create("preview", undefined, revision),
  accept: quoteAPI.accept,
  preview: (revision, quoteId) =>
    session.request(
      "/v1/courses/preview",
      "POST",
      {
        brief_revision: revision,
      },
      { "X-Quote-ID": quoteId },
    ),
};
const csp = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'none'; form-action 'none'; base-uri 'none'">`;
export function CoursePreview({
  revision,
  service = previewApi,
  onBack,
  onStep,
}: {
  revision: BriefRevision;
  service?: PreviewService;
  onBack: () => void;
  onStep?: (step: number) => void;
}) {
  const { reducedMotion, number } = useUiPreferences();
  const [quoted, setQuoted] = useState<{
    revision: string;
    quote: PreviewQuote;
  }>();
  const [preview, setPreview] = useState<Preview>();
  const [outline, setOutline] = useState(false);
  const [savedOutline, setSavedOutline] = useState<OutlineDraft | null>(null);
  const [workingOutline, setWorkingOutline] = useState(() =>
    readWorkingOutline(revision.id),
  );
  const [creation, setCreation] = useState<ConfirmedOutline>();
  const [editor, setEditor] = useState<{
    value: Outline;
    draft?: OutlineDraft;
  }>();
  useEffect(() => {
    let active = true;
    void outlineApi
      .latest(revision.id)
      .then((draft) => {
        if (active) setSavedOutline(draft);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [revision.id]);
  function editOutline() {
    if (!editor)
      setEditor(
        workingOutline
          ? { value: workingOutline.value, draft: workingOutline.draft }
          : savedOutline
            ? { value: savedOutline.outline, draft: savedOutline }
            : { value: sampleOutline(preview?.sample_outline.units ?? []) },
      );
    setOutline(true);
    onStep?.(3);
  }
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [index, setIndex] = useState(0);
  const [paidBalance, setPaidBalance] = useState<number>();
  const [playing, setPlaying] = useState(false);
  const [now, setNow] = useState(Date.now);
  const operation = useRef(0);
  const frame = useRef<HTMLIFrameElement>(null);
  const quote = quoted?.revision === revision.id ? quoted.quote : undefined;
  const expired = !!quote && Date.parse(quote.expires_at) <= now;
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    sendCommand(frame.current?.contentWindow ?? null, {
      type: "set_motion",
      reduced: reducedMotion,
    });
  }, [reducedMotion]);
  useEffect(
    () => () => {
      operation.current++;
    },
    [revision.id],
  );
  function back() {
    operation.current++;
    setQuoted(undefined);
    setPreview(undefined);
    setBusy(false);
    setError("");
    setOutline(false);
    onStep?.(1);
    onBack();
  }
  async function review() {
    const current = ++operation.current;
    setBusy(true);
    setError("");
    setQuoted(undefined);
    setPreview(undefined);
    setOutline(false);
    onStep?.(2);
    try {
      const [next, wallet] = await Promise.all([
        service.quote(revision.id),
        service.wallet?.().catch(() => undefined),
      ]);
      if (current === operation.current)
        setPaidBalance(
          wallet && !wallet.unverified
            ? wallet.purchased + wallet.subscription
            : undefined,
        );
      if (current === operation.current)
        setQuoted({ revision: revision.id, quote: next });
    } catch {
      if (current === operation.current)
        setError("Quote unavailable. Your brief is kept; connect and retry.");
    } finally {
      if (current === operation.current) setBusy(false);
    }
  }
  async function generate() {
    if (
      !quote ||
      expired ||
      busy ||
      (paidBalance !== undefined && paidBalance < quote.content_ceiling)
    )
      return;
    const current = ++operation.current;
    setBusy(true);
    setError("");
    try {
      await service.accept(quote.id);
      if (current !== operation.current) return;
      const result = await service.preview(revision.id, quote.id);
      if (current !== operation.current) return;
      if (
        !result.sample_outline?.units?.length ||
        !result.example_slides?.length
      )
        throw new Error("Invalid preview. Your brief is kept.");
      setPreview(result);
      setIndex(0);
      setPlaying(false);
      setQuoted(undefined);
    } catch (e) {
      if (current === operation.current) {
        setQuoted(undefined);
        setError(
          e instanceof Error
            ? e.message
            : "Preview unavailable. Your brief is kept; retry with a fresh quote.",
        );
      }
    } finally {
      if (current === operation.current) setBusy(false);
    }
  }
  const slide = preview?.example_slides[index];
  if (creation)
    return (
      <CourseCreate
        key={`${creation.draft_id}:${creation.revision}`}
        snapshot={creation}
        onBack={() => {
          setCreation(undefined);
          onStep?.(3);
        }}
      />
    );
  return (
    <section className="course-preview" aria-label="Personalized preview">
      {editor && (
        <div hidden={!outline} className="outline-surface">
          <CourseOutline
            briefRevision={revision.id}
            initialValue={editor.value}
            initialDraft={editor.draft}
            onConfirmed={(snapshot) => {
              setCreation(snapshot);
              onStep?.(4);
            }}
            onSaved={(draft) => {
              setSavedOutline(draft);
              setWorkingOutline(undefined);
            }}
            onBack={() => {
              setOutline(false);
              onStep?.(2);
            }}
          />
        </div>
      )}
      <div hidden={outline}>
        {(savedOutline || workingOutline) && !preview && (
          <Button onClick={editOutline}>
            {workingOutline ? "Resume unsaved outline" : "Resume saved outline"}
          </Button>
        )}
        {busy && <p role="status">Preparing preview… Your brief is kept.</p>}
        {error && <p role="alert">{error}</p>}
        {preview ? (
          <>
            <span className="sample-badge">
              Sample · No course has been created
            </span>
            <h2>Course preview</h2>
            <p>
              These examples show the approach. Lesson content is prepared one
              lesson at a time.
            </p>
            <>
              {slide && (
                <>
                  <iframe
                    ref={frame}
                    title={`Preview sample ${index + 1}`}
                    sandbox="allow-scripts"
                    srcDoc={csp + slide.html}
                    onLoad={() => {
                      sendCommand(frame.current?.contentWindow ?? null, {
                        type: "pause",
                      });
                      sendCommand(frame.current?.contentWindow ?? null, {
                        type: "set_motion",
                        reduced: reducedMotion,
                      });
                    }}
                  />
                  <p>{slide.narration}</p>
                  <p>
                    Sample {index + 1} of {preview.example_slides.length} ·
                    Starts paused. The explanation above remains available if
                    playback fails.
                  </p>
                  <Button
                    onClick={() => {
                      sendCommand(frame.current?.contentWindow ?? null, {
                        type: playing ? "pause" : "play",
                      });
                      setPlaying(!playing);
                    }}
                  >
                    {playing ? "Pause sample" : "Play sample"}
                  </Button>
                  <Button
                    variant="quiet"
                    disabled={index === 0}
                    onClick={() => {
                      setIndex(index - 1);
                      setPlaying(false);
                    }}
                  >
                    Previous sample
                  </Button>
                  <Button
                    variant="quiet"
                    disabled={index === preview.example_slides.length - 1}
                    onClick={() => {
                      setIndex(index + 1);
                      setPlaying(false);
                    }}
                  >
                    Next sample
                  </Button>
                </>
              )}
              <Button
                onClick={() => {
                  editOutline();
                }}
              >
                Review sample outline
              </Button>
              <Button variant="quiet" onClick={() => void review()}>
                Regenerate preview — review fresh cost
              </Button>
            </>
            <Button variant="quiet" onClick={back}>
              Discard preview
            </Button>
          </>
        ) : (
          <>
            {quote && (
              <div aria-live="polite">
                <h2>Ready to preview?</h2>
                <h3>{revision.brief.topic}</h3>
                <dl>
                  {briefFields
                    .filter((field) => revision.brief[field.id])
                    .map((field) => (
                      <div key={field.id}>
                        <dt>{field.label}</dt>
                        <dd>{revision.brief[field.id]}</dd>
                      </div>
                    ))}
                </dl>
                <p>
                  Up to {number(quote.content_ceiling)} credits · Original terms{" "}
                  {quote.pricing_version}
                </p>
                <p>
                  Purchased credits required. Daily free credits cannot pay for
                  previews.
                </p>
                {paidBalance !== undefined && (
                  <p>
                    Eligible paid balance {number(paidBalance)} →{" "}
                    {number(Math.max(0, paidBalance - quote.content_ceiling))}{" "}
                    credits
                  </p>
                )}
                {paidBalance !== undefined &&
                  paidBalance < quote.content_ceiling && (
                    <p role="alert">
                      Not enough eligible paid credits. Your brief is kept.
                    </p>
                  )}
                <p>
                  Your confirmed brief stays saved. This preview creates no
                  course or enrollment.
                </p>
                <p>Expires {new Date(quote.expires_at).toLocaleString()}</p>
                {expired ? (
                  <p role="alert">
                    Quote expired. Review a fresh quote before continuing.
                  </p>
                ) : (
                  <Button
                    disabled={
                      busy ||
                      (paidBalance !== undefined &&
                        paidBalance < quote.content_ceiling)
                    }
                    onClick={() => void generate()}
                  >
                    Confirm &amp; preview · {number(quote.content_ceiling)}
                  </Button>
                )}
              </div>
            )}
            <Button disabled={busy} onClick={() => void review()}>
              {quote ? "Renew preview quote" : "Review preview cost"}
            </Button>
            {(quote || error) && (
              <a href="/credits">Get credits — your brief is kept</a>
            )}
            <Button variant="quiet" onClick={back}>
              Back to brief
            </Button>
          </>
        )}
      </div>
    </section>
  );
}
