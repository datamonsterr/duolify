import { useEffect, useRef, useState } from "react";
import { track } from "../../lib/analytics";
import { isSlideMessage, nextSlide, sendCommand } from "./bridge";
import slide1 from "../../slides/one.html?raw";
import slide2 from "../../slides/two.html?raw";
import slide3 from "../../slides/three.html?raw";
const slides = [slide1, slide2, slide3];
export function LessonPlayer() {
  const [index, setIndex] = useState(0);
  const [finished, setFinished] = useState(false);
  const frame = useRef<HTMLIFrameElement>(null);
  useEffect(() => {
    track("lesson_started", { lesson: "sky" });
  }, []);
  useEffect(() => {
    track("slide_viewed", { slide: index + 1 });
  }, [index]);
  useEffect(() => {
    const listener = (event: MessageEvent) => {
      if (
        isSlideMessage(event, frame.current?.contentWindow ?? null) &&
        event.data.type === "ended"
      ) {
        /* Learner advances when ready. */
      }
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }, []);
  if (finished)
    return (
      <div className="complete">
        <div className="big-star">✦</div>
        <h1>Lesson complete!</h1>
        <p>Wonderful work. Your universe just got a little bigger.</p>
        <a href="/course/astronomy">Back to your path</a>
      </div>
    );
  return (
    <div className="player">
      <a href="/course/astronomy">← Exit lesson</a>
      <div className="progress">
        <span style={{ width: `${((index + 1) / slides.length) * 100}%` }} />
      </div>
      <p>
        Slide {index + 1} of {slides.length}
      </p>
      <iframe
        ref={frame}
        title={`Slide ${index + 1}`}
        sandbox="allow-scripts"
        srcDoc={slides[index]}
      />
      <div className="controls">
        <button
          onClick={() => setIndex(Math.max(0, index - 1))}
          disabled={index === 0}
        >
          Previous
        </button>
        <button
          onClick={() =>
            sendCommand(frame.current?.contentWindow ?? null, {
              type: "seek",
              time: 0,
            })
          }
        >
          Replay
        </button>
        <button
          onClick={() =>
            sendCommand(frame.current?.contentWindow ?? null, { type: "pause" })
          }
        >
          Pause
        </button>
        <button
          onClick={() =>
            sendCommand(frame.current?.contentWindow ?? null, { type: "play" })
          }
        >
          Play
        </button>
        <button
          className="primary"
          onClick={() => {
            if (index === slides.length - 1) {
              track("lesson_completed", { lesson: "sky" });
              setFinished(true);
            } else setIndex(nextSlide(index, slides.length));
          }}
        >
          {index === slides.length - 1 ? "Finish lesson" : "Next"}
        </button>
      </div>
      {index + 1 < slides.length && (
        <iframe
          title="Preloaded slide"
          hidden
          sandbox="allow-scripts"
          srcDoc={slides[index + 1]}
        />
      )}
    </div>
  );
}
