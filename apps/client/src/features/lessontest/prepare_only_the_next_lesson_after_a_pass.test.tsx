import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { LessonTest } from "./LessonTest";

import { http, HttpResponse } from "msw";
import { server } from "../../test/setup";

describe("prepare only the next lesson after a pass - LessonTest & Finish", () => {
  beforeEach(() => {
    server.use(
      http.get("*/v1/gamification/lessons/l1", () =>
        HttpResponse.json({ xp: 15, perfect: true }),
      ),
      http.get("*/v1/gamification", () =>
        HttpResponse.json({
          total_xp: 15,
          daily_xp: 15,
          daily_goal: 20,
          goal_options: [10, 20],
          goal_met: false,
          local_date: "2026-11-10",
          time_zone: "UTC",
        }),
      ),
    );
  });
  const mockQuestions = [
    {
      question_id: "q1",
      topic_id: "top-coords",
      prompt: "What is celestial altitude measured from?",
      choices: ["The horizon", "The zenith", "The celestial pole"],
    },
  ];

  it("renders S12-Finish screen with authoritative XP and daily goal after pass", async () => {
    const onPass = vi.fn();
    const onAnswer = vi.fn().mockResolvedValue({
      correct: true,
      answer_index: 0,
      explanation: "Celestial altitude is measured from the local horizon.",
      resolved_topics: 1,
      total_topics: 1,
      retry_queue_count: 0,
      passed: true,
    });

    render(
      <LessonTest
        lessonId="l1"
        courseId="c1"
        title="Celestial Coordinates"
        questions={mockQuestions}
        onPass={onPass}
        onExit={vi.fn()}
        onAnswer={onAnswer}
      />,
    );

    // Select correct choice and check
    fireEvent.click(screen.getByLabelText("The horizon"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    expect(await screen.findByText(/Correct/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    // Finish screen matching S12-Finish
    expect(
      await screen.findByRole("heading", { name: /Lesson complete/i }),
    ).toBeInTheDocument();
    expect(await screen.findByText(/\+15 XP/i)).toBeInTheDocument();
    expect(await screen.findByText("15 / 20 XP today")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /Back to learn path/i }),
    ).toBeInTheDocument();
  });

  it("displays S12-Finish insufficient credits state with 'See credit options'", async () => {
    const onPass = vi.fn();
    const onAnswer = vi.fn().mockResolvedValue({
      correct: true,
      answer_index: 0,
      explanation: "Good job",
      resolved_topics: 1,
      total_topics: 1,
      retry_queue_count: 0,
      passed: true,
      next_lesson_status: "insufficient_credits",
      required_credits: 190,
      available_credits: 12,
    });

    render(
      <LessonTest
        lessonId="l1"
        courseId="c1"
        title="Celestial Coordinates"
        questions={mockQuestions}
        onPass={onPass}
        onExit={vi.fn()}
        onAnswer={onAnswer}
      />,
    );

    fireEvent.click(screen.getByLabelText("The horizon"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));
    fireEvent.click(await screen.findByRole("button", { name: /Continue/i }));

    // Canvas S12-Finish state:
    // "Next lesson needs 190 credits"
    // "Purchased balance: 12. Your progress is saved; prepare it whenever you're ready."
    // "See credit options"
    expect(
      await screen.findByText(/Next lesson needs 190 credits/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /Purchased balance: 12\. Your progress is saved; prepare it whenever you're ready\./i,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /See credit options/i }),
    ).toBeInTheDocument();
    // Completion stands
    expect(
      screen.getByRole("heading", { name: /Lesson complete/i }),
    ).toBeInTheDocument();
  });

  it("displays S12-Finish failure state with retry estimate button without undoing completion", async () => {
    const onPass = vi.fn();
    const onAnswer = vi.fn().mockResolvedValue({
      correct: true,
      answer_index: 0,
      explanation: "Good job",
      resolved_topics: 1,
      total_topics: 1,
      retry_queue_count: 0,
      passed: true,
      next_lesson_status: "failed",
    });

    render(
      <LessonTest
        lessonId="l1"
        courseId="c1"
        title="Celestial Coordinates"
        questions={mockQuestions}
        onPass={onPass}
        onExit={vi.fn()}
        onAnswer={onAnswer}
      />,
    );

    fireEvent.click(screen.getByLabelText("The horizon"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));
    fireEvent.click(await screen.findByRole("button", { name: /Continue/i }));

    // Canvas S12-Finish state:
    // "Failure variant · next lesson couldn't be prepared"
    // "Reserved credits were released. This lesson is still complete."
    // "Try again · new estimate"
    expect(
      await screen.findByText(/next lesson couldn't be prepared/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /Reserved credits were released\. This lesson is still complete\./i,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Try again · new estimate/i }),
    ).toBeInTheDocument();
    // Completion stands
    expect(
      screen.getByRole("heading", { name: /Lesson complete/i }),
    ).toBeInTheDocument();
  });

  it("supports keyboard selection (number keys) and Enter checking", async () => {
    const onAnswer = vi.fn().mockResolvedValue({
      correct: true,
      answer_index: 0,
      explanation: "Correct",
      resolved_topics: 1,
      total_topics: 1,
      passed: true,
    });

    render(
      <LessonTest
        lessonId="l1"
        courseId="c1"
        title="Celestial Coordinates"
        questions={mockQuestions}
        onPass={vi.fn()}
        onExit={vi.fn()}
        onAnswer={onAnswer}
      />,
    );

    // Press '1' to select first choice
    fireEvent.keyDown(window, { key: "1" });
    const firstOption = screen.getByLabelText(
      "The horizon",
    ) as HTMLInputElement;
    expect(firstOption.checked).toBe(true);

    // Press 'Enter' to submit
    fireEvent.keyDown(window, { key: "Enter" });
    expect(onAnswer).toHaveBeenCalledWith("q1", 0);
  });
});
