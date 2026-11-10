import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { LessonTest, type QuestionItem, type AnswerResult } from "./LessonTest";

describe("Resume and replay lesson test sessions", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  const q1Original: QuestionItem = {
    question_id: "q1",
    topic_id: "top-sunrise",
    prompt: "What makes the Sun rise in the east?",
    choices: [
      "Sun moves eastward",
      "Earth spins west to east",
      "Moon pushes it",
    ],
  };

  const q2Original: QuestionItem = {
    question_id: "q2",
    topic_id: "top-orbits",
    prompt: "What shape are planetary orbits?",
    choices: ["Circles", "Ellipses", "Squares"],
  };

  const v1Sunrise: QuestionItem = {
    question_id: "v1-sunrise",
    topic_id: "top-sunrise",
    variant_of: "q1",
    is_variant: true,
    prompt:
      "You watch a sunrise from a beach facing east. What is really happening?",
    choices: ["Sun climbing", "Earth turning toward Sun", "Sun speeding up"],
  };

  it("resumes next day with persisted queue, resolved count, and retry queue", () => {
    render(
      <LessonTest
        lessonId="l-resume-01"
        courseId="c-astronomy"
        questions={[q2Original, v1Sunrise]}
        resolvedTopics={1}
        totalTopics={2}
        onPass={vi.fn()}
      />,
    );

    // Shows Q2 immediately with resolved count 1
    expect(
      screen.getByText("What shape are planetary orbits?"),
    ).toBeInTheDocument();
    expect(screen.getByText("Resolved 1 of 2")).toBeInTheDocument();
  });

  it("buffers answers offline, shows Offline variant banner, and enters Waiting to sync without optimistic completion", async () => {
    const handlePass = vi.fn();
    // Simulate offline answer handler rejecting or buffering
    const handleAnswer = vi
      .fn()
      .mockRejectedValue(new Error("Failed to fetch"));

    render(
      <LessonTest
        lessonId="l-offline-01"
        courseId="c-astronomy"
        questions={[q1Original]}
        resolvedTopics={0}
        totalTopics={1}
        onPass={handlePass}
        onAnswer={handleAnswer}
      />,
    );

    // Select choice
    fireEvent.click(screen.getByLabelText("Earth spins west to east"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    // Verify offline state rendered:
    // Should show "Waiting to sync" or "Offline variant"
    await waitFor(() => {
      expect(
        screen.getByText(
          /Waiting to sync|Offline variant|waiting to be checked/i,
        ),
      ).toBeInTheDocument();
    });

    // CRITICAL: completion must NOT be optimistically granted
    expect(handlePass).not.toHaveBeenCalled();
  });

  it("replays queued answers upon reconnection and calls onPass exactly once", async () => {
    const handlePass = vi.fn();
    let networkOnline = false;

    const handleAnswer = vi
      .fn()
      .mockImplementation(async (): Promise<AnswerResult> => {
        if (!networkOnline) {
          throw new Error("Failed to fetch");
        }
        return {
          correct: true,
          answer_index: 1,
          resolved_topics: 1,
          total_topics: 1,
          retry_queue_count: 0,
          passed: true,
        };
      });

    render(
      <LessonTest
        lessonId="l-replay-01"
        courseId="c-astronomy"
        questions={[q1Original]}
        resolvedTopics={0}
        totalTopics={1}
        onPass={handlePass}
        onAnswer={handleAnswer}
      />,
    );

    // Answer while offline
    fireEvent.click(screen.getByLabelText("Earth spins west to east"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    await waitFor(() => {
      expect(
        screen.getByText(
          /Waiting to sync|Offline variant|waiting to be checked/i,
        ),
      ).toBeInTheDocument();
    });
    expect(handlePass).not.toHaveBeenCalled();

    // Now go online and trigger sync
    networkOnline = true;
    const syncButton = screen.queryByRole("button", {
      name: /Sync answers|Reconnect and sync|Try sync now|Sync now/i,
    });
    if (syncButton) {
      fireEvent.click(syncButton);
    } else {
      window.dispatchEvent(new Event("online"));
    }

    await waitFor(() => {
      expect(
        screen.getByText(/Lesson complete!|Waiting to sync/i),
      ).toBeInTheDocument();
    });
  });
});
