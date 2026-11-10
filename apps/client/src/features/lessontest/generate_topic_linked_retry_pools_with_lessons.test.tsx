import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { LessonTest } from "./LessonTest";

describe("generate topic-linked retry pools with lessons - LessonTest", () => {
  const q1 = {
    question_id: "q1",
    topic_id: "top-sunrise",
    prompt: "Why does the Sun rise in the east?",
    choices: [
      "Sun moves east",
      "Earth rotates east",
      "Earth reverses direction",
    ],
  };

  const q2 = {
    question_id: "q2",
    topic_id: "top-orbits",
    prompt: "What shape are planetary orbits?",
    choices: ["Circles", "Ellipses", "Squares"],
  };

  const q1Variant = {
    question_id: "v1-q1",
    topic_id: "top-sunrise",
    variant_of: "q1",
    is_variant: true,
    prompt: "You watch a sunrise from a beach. What is happening?",
    choices: [
      "Sun climbs out of sea",
      "Earth turns toward the Sun",
      "Sun speeds up",
    ],
  };

  it("renders status chips and handles retry variant queueing on wrong answer", async () => {
    const onAnswer = vi
      .fn()
      // First answer: wrong on Q1
      .mockResolvedValueOnce({
        correct: false,
        answer_index: 1,
        explanation: "Earth rotates east toward the Sun.",
        resolved_topics: 0,
        total_topics: 2,
        retry_queue_count: 1,
        passed: false,
        retry_variant: q1Variant,
      })
      // Second answer: correct on Q2
      .mockResolvedValueOnce({
        correct: true,
        answer_index: 1,
        explanation: "Planetary orbits form ellipses.",
        resolved_topics: 1,
        total_topics: 2,
        retry_queue_count: 1,
        passed: false,
      })
      // Third answer: correct on Q1 variant
      .mockResolvedValueOnce({
        correct: true,
        answer_index: 1,
        explanation: "Earth rotation brings sunrise.",
        resolved_topics: 2,
        total_topics: 2,
        retry_queue_count: 0,
        passed: true,
      });

    const onPass = vi.fn();

    render(
      <LessonTest
        lessonId="l1"
        courseId="c1"
        title="Sun & Orbits"
        questions={[q1, q2]}
        onPass={onPass}
        onExit={vi.fn()}
        onAnswer={onAnswer}
      />,
    );

    // Initial chips
    expect(screen.getByText("Resolved 0 of 2")).toBeInTheDocument();
    expect(screen.getByText("Retry queue 0")).toBeInTheDocument();

    // Q1 prompt is displayed
    expect(
      screen.getByText("Why does the Sun rise in the east?"),
    ).toBeInTheDocument();

    // Select wrong choice for Q1
    fireEvent.click(screen.getByLabelText("Sun moves east"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    // Instant correction sheet
    expect(
      await screen.findByText(/Not quite — here's why/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Earth rotates east toward the Sun."),
    ).toBeInTheDocument();

    // Click Continue
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    // Min gap rule: Q2 is served next, NOT the retry variant immediately!
    expect(
      await screen.findByText("What shape are planetary orbits?"),
    ).toBeInTheDocument();
    expect(screen.getByText("Retry queue 1")).toBeInTheDocument();

    // Answer Q2 correctly
    fireEvent.click(screen.getByLabelText("Ellipses"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));
    expect(await screen.findByText(/Correct/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    // Now the pre-generated paraphrase retry variant for Q1 is served!
    expect(
      await screen.findByText(
        "You watch a sunrise from a beach. What is happening?",
      ),
    ).toBeInTheDocument();

    // Verification of S10-Test canvas retry badge
    expect(
      screen.getByText(/Retry variant · Same idea, fresh wording/i),
    ).toBeInTheDocument();
    expect(screen.getByText("Resolved 1 of 2")).toBeInTheDocument();

    // Answer retry variant correctly
    fireEvent.click(screen.getByLabelText("Earth turns toward the Sun"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));
    expect(await screen.findByText(/Correct/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    // Test passes
    expect(onPass).toHaveBeenCalled();
  });
});
