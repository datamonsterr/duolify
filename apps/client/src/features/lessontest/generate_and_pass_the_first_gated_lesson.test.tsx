import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { LessonTest } from "./LessonTest";

describe("generate and pass the first gated lesson - LessonTest", () => {
  const mockQuestions = [
    {
      question_id: "q1",
      topic_id: "top-stars",
      prompt: "What makes stars shine?",
      choices: ["Nuclear fusion", "Reflected sunlight", "Space dust"],
    },
  ];

  it("renders question and choices without revealing the correct answer", () => {
    render(
      <LessonTest
        lessonId="l1"
        courseId="c1"
        title="Night Sky"
        questions={mockQuestions}
        onPass={vi.fn()}
        onExit={vi.fn()}
        onAnswer={vi.fn()}
      />,
    );

    expect(screen.getByText("What makes stars shine?")).toBeInTheDocument();
    expect(screen.getByLabelText("Nuclear fusion")).toBeInTheDocument();
    expect(screen.getByLabelText("Reflected sunlight")).toBeInTheDocument();
    expect(screen.getByLabelText("Space dust")).toBeInTheDocument();

    // Check button should be disabled when no option selected
    const checkButton = screen.getByRole("button", { name: /Check/i });
    expect(checkButton).toBeDisabled();
  });

  it("enables check button on option selection and shows correction on wrong answer", async () => {
    const onAnswer = vi.fn().mockResolvedValue({
      correct: false,
      answer_index: 0,
      explanation: "Stars generate light via nuclear fusion at their cores.",
      resolved_topics: 0,
      total_topics: 1,
      retry_queue_count: 1,
      passed: false,
    });

    render(
      <LessonTest
        lessonId="l1"
        courseId="c1"
        title="Night Sky"
        questions={mockQuestions}
        onPass={vi.fn()}
        onExit={vi.fn()}
        onAnswer={onAnswer}
      />,
    );

    const wrongChoice = screen.getByLabelText("Reflected sunlight");
    fireEvent.click(wrongChoice);

    const checkButton = screen.getByRole("button", { name: /Check/i });
    expect(checkButton).not.toBeDisabled();

    fireEvent.click(checkButton);

    expect(onAnswer).toHaveBeenCalledWith("q1", 1);
    expect(
      await screen.findByText(/Not quite — here's why/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Stars generate light via nuclear fusion at their cores.",
      ),
    ).toBeInTheDocument();
  });

  it("handles correct answer and passes when all topics resolved", async () => {
    const onPass = vi.fn();
    const onAnswer = vi.fn().mockResolvedValue({
      correct: true,
      answer_index: 0,
      explanation: "Correct! Stars generate light via nuclear fusion.",
      resolved_topics: 1,
      total_topics: 1,
      retry_queue_count: 0,
      passed: true,
    });

    render(
      <LessonTest
        lessonId="l1"
        courseId="c1"
        title="Night Sky"
        questions={mockQuestions}
        onPass={onPass}
        onExit={vi.fn()}
        onAnswer={onAnswer}
      />,
    );

    const correctChoice = screen.getByLabelText("Nuclear fusion");
    fireEvent.click(correctChoice);

    const checkButton = screen.getByRole("button", { name: /Check/i });
    fireEvent.click(checkButton);

    expect(await screen.findByText(/Correct/i)).toBeInTheDocument();

    const continueButton = screen.getByRole("button", { name: /Continue/i });
    fireEvent.click(continueButton);

    expect(onPass).toHaveBeenCalled();
  });
});
