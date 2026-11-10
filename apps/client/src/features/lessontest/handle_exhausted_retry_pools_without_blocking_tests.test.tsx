import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { LessonTest, type QuestionItem, type AnswerResult } from "./LessonTest";

describe("Handle exhausted retry pools without blocking tests", () => {
  const q1Original: QuestionItem = {
    question_id: "q1",
    topic_id: "top-sunrise",
    prompt: "What makes the Sun rise in the east?",
    choices: [
      "The Sun orbits Earth daily.",
      "Earth spins west to east.",
      "The Moon pushes it up.",
    ],
  };

  const v1Sunrise: QuestionItem = {
    question_id: "v1-sunrise",
    topic_id: "top-sunrise",
    variant_of: "q1",
    is_variant: true,
    prompt:
      "You watch a sunrise from a beach facing east. What is really happening?",
    choices: [
      "The Sun is climbing out of the sea.",
      "Your side of Earth is turning toward the Sun.",
      "The Sun speeds up each morning.",
    ],
  };

  const vReorderedFallback: QuestionItem = {
    question_id: "v1-sunrise-reordered-2",
    topic_id: "top-sunrise",
    variant_of: "q1",
    is_variant: true,
    prompt:
      "You watch a sunrise from a beach facing east. What is really happening?",
    choices: [
      "Your side of Earth is turning toward the Sun.",
      "The Sun speeds up each morning.",
      "The Sun is climbing out of the sea.",
    ],
  };

  it("handles pool exhaustion by queuing reordered variant for free without auto-passing", async () => {
    let callCount = 0;
    const handleAnswer = vi
      .fn()
      .mockImplementation(async (qId: string): Promise<AnswerResult> => {
        callCount++;
        if (qId === "q1") {
          return {
            correct: false,
            answer_index: 1,
            explanation: "Earth turns west to east.",
            resolved_topics: 0,
            total_topics: 1,
            retry_queue_count: 1,
            passed: false,
            retry_variant: v1Sunrise,
          };
        }
        if (qId === "v1-sunrise") {
          // Pool is now exhausted -> serves recycled variant with reordered choices
          return {
            correct: false,
            answer_index: 1,
            explanation: "Your side of Earth turns toward the Sun.",
            resolved_topics: 0,
            total_topics: 1,
            retry_queue_count: 1,
            passed: false,
            retry_variant: vReorderedFallback,
          };
        }
        if (qId === "v1-sunrise-reordered-2") {
          return {
            correct: true,
            answer_index: 0,
            explanation: "Earth rotation brings daylight.",
            resolved_topics: 1,
            total_topics: 1,
            retry_queue_count: 0,
            passed: true,
          };
        }
        throw new Error("unexpected question");
      });

    const onPass = vi.fn();
    render(
      <LessonTest
        lessonId="lesson-e407"
        questions={[q1Original]}
        onAnswer={handleAnswer}
        onPass={onPass}
      />,
    );

    // Initial Q1
    fireEvent.click(screen.getByLabelText("The Sun orbits Earth daily."));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    await waitFor(() => {
      expect(screen.getByText(/Not quite — here's why/i)).toBeDefined();
    });
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    // v1-sunrise is served
    await waitFor(() => {
      expect(
        screen.getByText(
          "You watch a sunrise from a beach facing east. What is really happening?",
        ),
      ).toBeDefined();
    });
    expect(
      screen.getByText(/Retry variant · Same idea, fresh wording/i),
    ).toBeDefined();

    // Answer v1-sunrise wrongly -> pool exhausted!
    fireEvent.click(screen.getByLabelText("The Sun speeds up each morning."));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    await waitFor(() => {
      expect(screen.getByText(/Not quite — here's why/i)).toBeDefined();
    });
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    // Recycled variant with reordered choices is served
    await waitFor(() => {
      expect(
        screen.getByLabelText("Your side of Earth is turning toward the Sun."),
      ).toBeDefined();
    });

    // Never auto-pass: topic must NOT be resolved before correct submission
    expect(onPass).not.toHaveBeenCalled();

    // Submit correct answer on recycled variant
    fireEvent.click(
      screen.getByLabelText("Your side of Earth is turning toward the Sun."),
    );
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    await waitFor(() => {
      expect(screen.getByText(/Correct/i)).toBeDefined();
    });
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    expect(onPass).toHaveBeenCalled();
    expect(callCount).toBe(3);
  });

  it("displays canvas S10-Test variant failure alert and recovers on Retry click", async () => {
    let failOnce = true;
    const handleAnswer = vi
      .fn()
      .mockImplementation(async (): Promise<AnswerResult> => {
        if (failOnce) {
          failOnce = false;
          throw new Error("Provider rate limited or unavailable");
        }
        return {
          correct: false,
          answer_index: 1,
          explanation: "Earth rotates west to east.",
          resolved_topics: 0,
          total_topics: 1,
          retry_queue_count: 1,
          passed: false,
          retry_variant: v1Sunrise,
        };
      });

    const onExit = vi.fn();
    render(
      <LessonTest
        lessonId="lesson-e407"
        questions={[q1Original]}
        onAnswer={handleAnswer}
        onPass={vi.fn()}
        onExit={onExit}
      />,
    );

    fireEvent.click(screen.getByLabelText("The Sun orbits Earth daily."));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    // Should display Canvas S10-Test failure banner
    await waitFor(() => {
      expect(
        screen.getByText("Couldn't prepare the retry question"),
      ).toBeDefined();
    });
    expect(
      screen.getByText(
        "Your progress and queue are saved. No credits are charged for retries.",
      ),
    ).toBeDefined();

    // Verify recovery buttons
    const retryBtn = screen.getByRole("button", { name: "Retry" });
    const returnBtn = screen.getByRole("button", {
      name: /Return to lesson|Exit to lesson/i,
    });
    expect(retryBtn).toBeDefined();
    expect(returnBtn).toBeDefined();

    // Clicking Retry action recovers and continues
    fireEvent.click(retryBtn);

    await waitFor(() => {
      expect(screen.getByText(/Not quite — here's why/i)).toBeDefined();
    });
    expect(
      screen.queryByText("Couldn't prepare the retry question"),
    ).toBeNull();
  });

  it("calls onExit when Return to lesson is clicked on failure banner", async () => {
    const handleAnswer = vi.fn().mockRejectedValue(new Error("Network error"));

    const onExit = vi.fn();
    render(
      <LessonTest
        lessonId="lesson-e407"
        questions={[q1Original]}
        onAnswer={handleAnswer}
        onPass={vi.fn()}
        onExit={onExit}
      />,
    );

    fireEvent.click(screen.getByLabelText("The Sun orbits Earth daily."));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    await waitFor(() => {
      expect(
        screen.getByText("Couldn't prepare the retry question"),
      ).toBeDefined();
    });

    const returnBtn = screen.getByRole("button", {
      name: /Return to lesson|Exit to lesson/i,
    });
    fireEvent.click(returnBtn);
    expect(onExit).toHaveBeenCalled();
  });
});
