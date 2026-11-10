import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import { sandboxDocument } from "../player/manifest";
import { PlaybackClock } from "../player/playback";
import { sendCommand } from "../lesson-player/bridge";
import { trackSharedDemo } from "../../lib/analytics";
import type { Preview } from "./CoursePreview";
import "./course-brief.css";

type Demo = { kind: "shared_demo"; version: "sky-v1"; preview: Preview };
const base = import.meta.env.VITE_API_URL || "http://localhost:8080";
async function loadDemo(signal: AbortSignal): Promise<Demo> {
  // Public authored content: no authorization, profile or draft is sent.
  const response = await fetch(`${base}/v1/courses/shared-demo`, {
    signal,
    credentials: "omit",
  });
  if (!response.ok) throw new Error("demo_unavailable");
  const data = (await response.json()) as Demo;
  if (
    data.kind !== "shared_demo" ||
    data.version !== "sky-v1" ||
    !data.preview?.sample_outline?.title ||
    !data.preview.sample_outline.units?.length ||
    !data.preview.sample_outline.units.every(
      (unit) =>
        typeof unit.title === "string" &&
        unit.lessons?.length &&
        unit.lessons.every(
          (lesson) =>
            typeof lesson.title === "string" && typeof lesson.goal === "string",
        ),
    ) ||
    !data.preview.example_slides?.length ||
    data.preview.example_slides.length > 5 ||
    !data.preview.example_slides.every(
      (slide) =>
        typeof slide.html === "string" &&
        slide.html.length < 262144 &&
        typeof slide.narration === "string" &&
        slide.duration_ms > 0 &&
        slide.duration_ms <= 60000,
    )
  )
    throw new Error("invalid_demo");
  return data;
}
export function SharedDemo({
  surface,
  onReturn,
}: {
  surface: "welcome" | "course_brief";
  onReturn: () => void;
}) {
  const { reducedMotion } = useUiPreferences();
  const [demo, setDemo] = useState<Demo>();
  const [failed, setFailed] = useState(false);
  const [retry, setRetry] = useState(0);
  const [index, setIndex] = useState(0);
  const heading = useRef<HTMLHeadingElement>(null);
  const viewed = useRef(false);
  const opened = useRef(false);
  useEffect(() => {
    heading.current?.focus();
    if (!opened.current) {
      trackSharedDemo(surface);
      opened.current = true;
    }
  }, [surface]);
  useEffect(() => {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 10000);
    void loadDemo(controller.signal)
      .then((value) => {
        if (controller.signal.aborted) return;
        setDemo(value);
        setFailed(false);
        if (!viewed.current) {
          trackSharedDemo(surface, true);
          viewed.current = true;
        }
      })
      .catch(() => {
        setFailed(true);
      })
      .finally(() => clearTimeout(timer));
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [retry, surface]);
  const slide = demo?.preview.example_slides[index];
  return (
    <section
      className="shared-demo course-preview"
      aria-labelledby="shared-demo-title"
    >
      <span className="sample-badge">Shared example · No credits needed</span>
      <h2 id="shared-demo-title" tabIndex={-1} ref={heading}>
        Shared example
      </h2>
      <p>
        A fixed authored example for everyone. It is not personalized. No course
        is created and no credits are reserved or granted.
      </p>
      {!demo && !failed && (
        <p role="status">Loading shared example… Your draft is kept.</p>
      )}
      {failed && (
        <>
          <p role="alert">
            Shared example unavailable. Connect and retry; your draft is kept.
          </p>
          <Button
            onClick={() => {
              setFailed(false);
              setRetry((value) => value + 1);
            }}
          >
            Retry shared example
          </Button>
        </>
      )}
      {demo && (
        <div className="shared-demo-grid">
          <div>
            {slide && (
              <>
                <p>
                  Example {index + 1} of {demo.preview.example_slides.length}
                </p>
                <DemoSlide
                  key={slide.slide_id}
                  slide={slide}
                  index={index}
                  reduced={reducedMotion}
                />
                <div className="outline-actions">
                  <Button
                    variant="quiet"
                    disabled={index === 0}
                    onClick={() => {
                      setIndex((i) => i - 1);
                    }}
                  >
                    Previous example
                  </Button>
                  <Button
                    variant="quiet"
                    disabled={index === demo.preview.example_slides.length - 1}
                    onClick={() => {
                      setIndex((i) => i + 1);
                    }}
                  >
                    Next example
                  </Button>
                </div>
                <p>{slide.narration}</p>
                <details>
                  <summary>Readable transcript</summary>
                  <p>{slide.narration}</p>
                </details>
              </>
            )}
          </div>
          <aside aria-label="Shared sample outline">
            <h3>{demo.preview.sample_outline.title}</h3>
            <ol>
              {demo.preview.sample_outline.units.map((unit, i) => (
                <li key={i}>
                  {unit.title}
                  <ol>
                    {unit.lessons.map((lesson, j) => (
                      <li key={j}>
                        <strong>{lesson.title}</strong>
                        <p>{lesson.goal}</p>
                      </li>
                    ))}
                  </ol>
                </li>
              ))}
            </ol>
          </aside>
        </div>
      )}
      <p>
        Personalized paid previews use your confirmed brief and require a new
        quote and explicit consent.
      </p>
      <Button onClick={onReturn}>
        {surface === "welcome"
          ? "Return to welcome"
          : "Return to personalized creation"}
      </Button>
    </section>
  );
}

// Reuse E3's parent clock, sandbox policy and playback bridge, without lesson
// progress, cache, enrollment or completion side effects.
function DemoSlide({
  slide,
  index,
  reduced,
}: {
  slide: Preview["example_slides"][number];
  index: number;
  reduced: boolean;
}) {
  const [clock] = useState(() => new PlaybackClock(slide.duration_ms, 0));
  const state = useSyncExternalStore(clock.subscribe, clock.snapshot);
  const frame = useRef<HTMLIFrameElement>(null);
  useEffect(() => {
    if (!state.playing) return;
    const timer = setInterval(() => clock.tick(performance.now()), 100);
    return () => clearInterval(timer);
  }, [clock, state.playing]);
  useEffect(() => {
    sendCommand(frame.current?.contentWindow ?? null, {
      type: "tick",
      time: state.time,
    });
    sendCommand(frame.current?.contentWindow ?? null, {
      type: state.playing ? "play" : "pause",
    });
  }, [state.time, state.playing]);
  const metadata = {
    slide_id: slide.slide_id,
    duration_ms: slide.duration_ms,
    checkpoints: [],
    assets: [],
    asset_sha256: {},
    html_sha256: "",
    html_url: "",
    narration_status: "disabled" as const,
    narration_text: slide.narration,
  };
  return (
    <>
      <iframe
        ref={frame}
        title={`Shared example slide ${index + 1}`}
        sandbox="allow-scripts"
        referrerPolicy="no-referrer"
        srcDoc={sandboxDocument(
          `<style>svg{max-inline-size:100%;block-size:auto}</style>${slide.html}`,
          "https://assets.example.test",
          reduced,
          metadata,
          true,
        )}
      />
      <div className="outline-actions">
        <Button
          variant="secondary"
          onClick={() =>
            state.playing
              ? clock.pause(performance.now())
              : state.ended
                ? clock.replay(performance.now())
                : clock.play(performance.now())
          }
        >
          {state.playing ? "Pause example" : "Play example"}
        </Button>
        <Button variant="quiet" onClick={() => clock.replay(performance.now())}>
          Replay example
        </Button>
      </div>
    </>
  );
}
