import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import { trackOnboarding } from "../../lib/analytics";
import {
  advance,
  availableSteps,
  placeTooltip,
  targetFor,
  type Placement,
  type Rect,
  type StepId,
} from "./tour";
import { Modal } from "./Modal";

type Geometry = { target: Rect; placement: Placement; viewport: Rect };
const view = (): Rect => ({
  x: window.visualViewport?.offsetLeft ?? 0,
  y: window.visualViewport?.offsetTop ?? 0,
  width: window.visualViewport?.width ?? window.innerWidth,
  height: window.visualViewport?.height ?? window.innerHeight,
});
// Authored geometry: 8px gap around the real target, 16px rounded cutout.
function hole(rect: Rect) {
  const x = rect.x - 8,
    y = rect.y - 8,
    w = rect.width + 16,
    h = rect.height + 16,
    r = Math.min(16, w / 2, h / 2);
  return `M${x + r},${y}H${x + w - r}Q${x + w},${y} ${x + w},${y + r}V${y + h - r}Q${x + w},${y + h} ${x + w - r},${y + h}H${x + r}Q${x},${y + h} ${x},${y + h - r}V${y + r}Q${x},${y} ${x + r},${y}Z`;
}
export function CoachMarks({
  source,
  onFinish,
}: {
  source: "first_run" | "settings";
  onFinish: (state: "completed" | "skipped") => void;
}) {
  const { t, number } = useUiPreferences();
  const [steps, setSteps] = useState<StepId[]>(availableSteps);
  const [index, setIndex] = useState(0);
  const [geometry, setGeometry] = useState<Geometry | null>(null);
  const card = useRef<HTMLDivElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const step = steps[index];
  const finish = useRef(onFinish);
  useEffect(() => {
    finish.current = onFinish;
  });
  const hasGeometry = geometry !== null;
  const announced = useRef("");
  useEffect(() => {
    trackOnboarding({ event: "onboarding_tour_started", entry_point: source });
  }, [source]);
  useEffect(() => {
    if (!step) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      if (steps.length) setIndex(steps.length - 1);
      else finish.current("skipped");
      return;
    }
    // StrictMode and position updates must not double-count a viewed step.
    if (announced.current !== step) {
      trackOnboarding({ event: "onboarding_tour_step_viewed", step });
      announced.current = step;
    }
    heading.current?.focus({ preventScroll: true });
  }, [step, hasGeometry, steps.length]);
  useLayoutEffect(() => {
    if (!step) return;
    const target = targetFor(step);
    if (!target) {
      // The anchor only exists in the DOM, so absence is detected here.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setSteps((current) => current.filter((id) => id !== step));
      return;
    }
    target.scrollIntoView({
      behavior: "instant",
      block: "center",
      inline: "nearest",
    });
    const oldDescription = target.getAttribute("aria-describedby");
    target.setAttribute(
      "aria-describedby",
      [oldDescription, "onboarding-description"].filter(Boolean).join(" "),
    );
    target.dataset.onboardingActive = "true";
    let frame = 0;
    const measure = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (!targetFor(step)) {
          setSteps((current) => current.filter((id) => id !== step));
          return;
        }
        const rect = target.getBoundingClientRect(),
          viewport = view();
        const cardRect = card.current?.getBoundingClientRect();
        setGeometry({
          target: {
            x: rect.x,
            y: rect.y,
            width: rect.width,
            height: rect.height,
          },
          placement: placeTooltip(
            rect,
            {
              width: cardRect?.width ?? Math.min(352, viewport.width - 24),
              height: cardRect?.height ?? 320,
            },
            viewport,
          ),
          viewport,
        });
      });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(target);
    if (card.current) observer.observe(card.current);
    // A feature disappearing while the tour is open cannot leave an empty hole.
    const mutations = new MutationObserver(() => {
      if (!target.isConnected || !targetFor(step)) measure();
    });
    mutations.observe(document.getElementById("main-content")!, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["hidden", "class", "disabled"],
    });
    window.addEventListener("resize", measure);
    document.addEventListener("scroll", measure, true);
    window.visualViewport?.addEventListener("resize", measure);
    window.visualViewport?.addEventListener("scroll", measure);
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      mutations.disconnect();
      window.removeEventListener("resize", measure);
      document.removeEventListener("scroll", measure, true);
      window.visualViewport?.removeEventListener("resize", measure);
      window.visualViewport?.removeEventListener("scroll", measure);
      if (oldDescription === null) target.removeAttribute("aria-describedby");
      else target.setAttribute("aria-describedby", oldDescription);
      delete target.dataset.onboardingActive;
    };
  }, [step]);
  function move(action: "next" | "back" | "skip") {
    const result = advance(index, action, steps.length);
    if (typeof result === "number") setIndex(result);
    else {
      if (result === "skipped")
        trackOnboarding({ event: "onboarding_tour_skipped", at_step: step });
      else trackOnboarding({ event: "onboarding_tour_completed" });
      onFinish(result);
    }
  }
  if (!step) return null;
  const placement = geometry?.placement;
  const targetLabel =
    targetFor(step)?.getAttribute("aria-label") ||
    targetFor(step)?.textContent?.trim();
  return (
    <Modal
      label={t("onboarding.tour")}
      onSkip={() => move("skip")}
      testId="onboarding-tour"
      stepId={step}
      stepIndex={index + 1}
      total={steps.length}
      onKey={(event) => {
        if (event.key === "ArrowRight" || event.key === "ArrowLeft") {
          event.preventDefault();
          move(event.key === "ArrowRight" ? "next" : "back");
        }
        if (event.key === "Enter" && event.target === heading.current) {
          event.preventDefault();
          move("next");
        }
      }}
    >
      {geometry && (
        <svg
          className="onboarding-mask"
          aria-hidden="true"
          width="100%"
          height="100%"
        >
          <path
            fillRule="evenodd"
            d={`M0,0H${window.innerWidth}V${window.innerHeight}H0Z ${hole(geometry.target)}`}
          />
          <path className="onboarding-ring" d={hole(geometry.target)} />
        </svg>
      )}
      <div
        ref={card}
        className="onboarding-tour-card"
        data-testid="tour-card"
        style={
          {
            left: placement?.x ?? 12,
            top: placement?.y ?? 12,
            visibility: placement ? "visible" : "hidden",
            maxHeight: `calc(${geometry?.viewport.height ?? window.innerHeight}px - 24px)`,
            "--tour-arrow": `${placement?.arrow ?? 24}px`,
          } as CSSProperties
        }
      >
        <span
          className={`onboarding-arrow arrow-${placement?.side ?? "top"}`}
          aria-hidden="true"
        />
        <div className="onboarding-card-body">
          <div className="onboarding-step-row">
            <div className="onboarding-dots" aria-hidden="true">
              {steps.map((id, i) => (
                <span
                  key={id}
                  className={
                    i === index ? "current" : i < index ? "visited" : ""
                  }
                />
              ))}
            </div>
            <span>
              {t("onboarding.step", {
                current: number(index + 1),
                total: number(steps.length),
              })}
            </span>
          </div>
          <h2
            ref={heading}
            tabIndex={-1}
            aria-describedby="onboarding-description onboarding-target"
          >
            {t(`onboarding.${step}.title`)}
          </h2>
          <p id="onboarding-description">{t(`onboarding.${step}.body`)}</p>
          <span id="onboarding-target" className="sr-only">
            {targetLabel}
          </span>
          <div className="onboarding-actions">
            <Button
              variant="quiet"
              disabled={index === 0}
              onClick={() => move("back")}
            >
              {t("onboarding.back")}
            </Button>
            <Button onClick={() => move("next")}>
              {t(
                index + 1 === steps.length
                  ? "onboarding.done"
                  : "onboarding.next",
              )}
            </Button>
          </div>
          <Button
            variant="quiet"
            className="onboarding-skip"
            onClick={() => move("skip")}
          >
            {t("onboarding.skip")}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
