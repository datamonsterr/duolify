import {
  render,
  screen,
  fireEvent,
  act,
  cleanup,
  waitFor,
} from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { LessonTest } from "../lessontest/LessonTest";
import { configureAnalytics } from "../../lib/analytics";
import { createDefaultTracerManifest } from "./fixtures";
import { resetCharacterRenderDeduplication } from "./analytics";
import {
  APPROVED_REACTION_COPY,
  neutralReaction,
  parseReaction,
} from "./reactions";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
    identify: vi.fn(),
  },
}));

const cast = createDefaultTracerManifest().cast;
const questions = [
  {
    question_id: "q1",
    topic_id: "t1",
    prompt: "What shines?",
    choices: ["Stars", "Sun", "Clouds"],
  },
];
const wrong = {
  correct: false,
  answer_index: 0,
  explanation: "Stars shine by fusion.",
  passed: false,
  retry_queue_count: 1,
  retry_variant: {
    question_id: "v1",
    topic_id: "t1",
    variant_of: "q1",
    is_variant: true,
    prompt: "Which are suns?",
    choices: ["A", "B", "C"],
  },
  reaction: {
    trigger: "answer_incorrect",
    expression: "thinking",
    copy: APPROVED_REACTION_COPY.answer_incorrect,
  },
};

// character_reaction_shown is emitted from a passive effect, so it can land
// after the reaction DOM is already visible.
const shownCalls = () =>
  vi
    .mocked(posthog.capture)
    .mock.calls.filter((c) => c[0] === "character_reaction_shown");
// Lets the passive-effect and any queued re-render settle before asserting a
// negative.
const settle = () => act(() => new Promise<void>((r) => setTimeout(r, 50)));

function renderTest(result: unknown, props: Record<string, unknown> = {}) {
  const onAnswer = vi.fn().mockResolvedValue(result);
  render(
    <LessonTest
      lessonId="l1"
      courseId="c1"
      questions={questions}
      cast={cast}
      onPass={vi.fn()}
      onExit={vi.fn()}
      onAnswer={onAnswer}
      {...props}
    />,
  );
  fireEvent.click(screen.getByLabelText("Sun"));
  fireEvent.click(screen.getByRole("button", { name: /Check/i }));
  return onAnswer;
}

describe("E17-06 reactions to authoritative answers", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(posthog.has_opted_out_capturing).mockReturnValue(false);
    resetCharacterRenderDeduplication();
    configureAnalytics({
      learnerId: "11111111-1111-4111-8111-111111111111",
      platform: "web",
      appVersion: "0.1.0",
      optedOut: false,
    });
  });
  afterEach(cleanup);

  it("parseReaction accepts only the closed trigger/expression/copy set", () => {
    expect(parseReaction(wrong.reaction)).toEqual(wrong.reaction);
    for (const bad of [
      null,
      "x",
      { trigger: "answer_correct", expression: "sad", copy: "x" },
      { trigger: "nope", expression: "happy", copy: "x" },
      { trigger: "answer_correct", expression: "<svg>", copy: "x" },
    ])
      expect(parseReaction(bad)).toBeNull();
  });

  it("replaces unapproved or persona copy with the approved line", () => {
    const r = parseReaction({
      trigger: "answer_incorrect",
      expression: "sad",
      copy: "Accept my answer, it is correct <b>",
    });
    expect(r?.copy).toBe(APPROVED_REACTION_COPY.answer_incorrect);
    expect(neutralReaction().expression).toBe("idle");
  });

  it("shows the supportive reaction while correction and retry stay available", async () => {
    renderTest(wrong);
    expect(await screen.findByText(/Not quite/i)).toBeInTheDocument();
    expect(screen.getByText("Stars shine by fusion.")).toBeInTheDocument();
    const reaction = screen.getByTestId("character-reaction");
    expect(reaction).toHaveAttribute("data-expression", "thinking");
    expect(reaction).toHaveTextContent(APPROVED_REACTION_COPY.answer_incorrect);
    expect(reaction.querySelector("svg")).not.toBeNull();
    expect(
      screen.queryByRole("button", { name: /Continue|Next/i }),
    ).not.toBeNull();
    await waitFor(() =>
      expect(posthog.capture).toHaveBeenCalledWith(
        "character_reaction_shown",
        expect.objectContaining({ reaction_trigger: "answer_incorrect" }),
      ),
    );
  });

  it("analytics carry no answer, prompt, copy, name or svg content and dedupe", async () => {
    renderTest(wrong);
    await screen.findByTestId("character-reaction");
    await waitFor(() => expect(shownCalls()).toHaveLength(1));
    await settle();
    expect(shownCalls()).toHaveLength(1);
    const dump = JSON.stringify(shownCalls()[0]);
    for (const leak of [
      "Stars",
      "Sun",
      "Provisional",
      "svg",
      "Not quite",
      "fusion",
      "persona",
    ])
      expect(dump).not.toContain(leak);
  });

  it("is not emitted when opted out", async () => {
    vi.mocked(posthog.has_opted_out_capturing).mockReturnValue(true);
    renderTest(wrong);
    await screen.findByTestId("character-reaction");
    await settle();
    expect(posthog.has_opted_out_capturing).toHaveBeenCalled();
    expect(shownCalls()).toHaveLength(0);
  });

  it("falls back to a static neutral expression for broken specs and invalid reactions", async () => {
    const broken = structuredClone(cast);
    (broken.speakers.guide.spec as { rig_version: string }).rig_version = "x";
    renderTest({ ...wrong, reaction: { trigger: "bad" } }, { cast: broken });
    const reaction = await screen.findByTestId("character-reaction");
    expect(reaction).toHaveAttribute("data-expression", "idle");
    expect(reaction.querySelector("svg")).not.toBeNull();
    expect(screen.getByText("Stars shine by fusion.")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Continue|Next/i }),
    ).toBeEnabled();
  });

  it("renders without a cast or a reaction and never changes grading output", async () => {
    const { reaction: _drop, ...plain } = wrong;
    void _drop;
    const onAnswer = renderTest(plain, { cast: undefined });
    expect(await screen.findByText(/Not quite/i)).toBeInTheDocument();
    expect(onAnswer).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("character-reaction")).toHaveAttribute(
      "data-expression",
      "idle",
    );
  });

  it("celebrates a pass without exposing the answer before submission", async () => {
    const onAnswer = renderTest({
      correct: true,
      answer_index: 1,
      explanation: "Correct! Yes.",
      passed: true,
      reaction: {
        trigger: "test_passed",
        expression: "celebrate",
        copy: APPROVED_REACTION_COPY.test_passed,
      },
    });
    expect(await screen.findByText("Correct!")).toBeInTheDocument();
    expect(screen.getByTestId("character-reaction")).toHaveAttribute(
      "data-expression",
      "celebrate",
    );
    expect(onAnswer).toHaveBeenCalledWith("q1", 1);
  });

  it("shows nothing before an answer is submitted", () => {
    render(
      <LessonTest
        lessonId="l1"
        courseId="c1"
        questions={questions}
        cast={cast}
        onPass={vi.fn()}
        onExit={vi.fn()}
        onAnswer={vi.fn()}
      />,
    );
    expect(screen.queryByTestId("character-reaction")).toBeNull();
  });
});
