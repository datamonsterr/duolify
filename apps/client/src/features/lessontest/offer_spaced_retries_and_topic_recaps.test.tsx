import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { LessonTest, type QuestionItem, type AnswerResult } from "./LessonTest";

describe("Offer spaced retries and topic recaps", () => {
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

  it("spaces retry variant after min_gap questions when questions remain", async () => {
    const handleAnswer = vi
      .fn()
      .mockImplementation(async (qId: string): Promise<AnswerResult> => {
        if (qId === "q1") {
          return {
            correct: false,
            answer_index: 1,
            explanation: "Earth rotates west to east.",
            resolved_topics: 0,
            total_topics: 2,
            retry_queue_count: 1,
            passed: false,
            retry_variant: v1Sunrise,
          };
        }
        return {
          correct: true,
          answer_index: 1,
          resolved_topics: 1,
          total_topics: 2,
          retry_queue_count: 1,
          passed: false,
        };
      });

    render(
      <LessonTest
        lessonId="lesson-1"
        questions={[q1Original, q2Original]}
        onPass={vi.fn()}
        onAnswer={handleAnswer}
      />,
    );

    // Initial question is Q1
    expect(
      screen.getByText("What makes the Sun rise in the east?"),
    ).toBeDefined();

    // Select wrong choice for Q1
    fireEvent.click(screen.getByLabelText("Sun moves eastward"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    // Correction sheet appears with explanation and retry count
    await waitFor(() => {
      expect(screen.getByText(/Not quite — here's why/i)).toBeDefined();
    });
    expect(screen.getByText("Earth rotates west to east.")).toBeDefined();

    // Click continue
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    // Question 2 appears next (min_gap=2 spacing: variant is not served immediately!)
    await waitFor(() => {
      expect(
        screen.getByText("What shape are planetary orbits?"),
      ).toBeDefined();
    });
  });

  it("offers topic recap after max_variants_per_topic consecutive wrong attempts without resolving topic", async () => {
    let attempts = 0;
    const handleAnswer = vi
      .fn()
      .mockImplementation(async (): Promise<AnswerResult> => {
        attempts++;
        if (attempts < 3) {
          return {
            correct: false,
            answer_index: 1,
            explanation: "Wrong",
            resolved_topics: 0,
            total_topics: 1,
            retry_queue_count: 1,
            passed: false,
            retry_variant: { ...v1Sunrise, question_id: `v-${attempts}` },
          };
        }
        // 3rd consecutive wrong variant -> offer recap!
        return {
          correct: false,
          answer_index: 1,
          explanation: "Wrong again",
          resolved_topics: 0,
          total_topics: 1,
          retry_queue_count: 1,
          passed: false,
          offer_recap: true,
          recap_slides: [
            {
              slide_id: "s1",
              html: '<div id="root"><h1>Earth Spin</h1><p>Earth rotates eastward.</p></div>',
              duration_ms: 3000,
              narration: "Earth rotates eastward.",
              topic_ids: ["top-sunrise"],
            },
          ],
          retry_variant: { ...v1Sunrise, question_id: "v-after-recap" },
        };
      });

    render(
      <LessonTest
        lessonId="lesson-1"
        questions={[q1Original]}
        onPass={vi.fn()}
        onAnswer={handleAnswer}
      />,
    );

    // Attempt 1
    fireEvent.click(screen.getByLabelText("Sun moves eastward"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));
    await waitFor(() => screen.getByRole("button", { name: /Continue/i }));
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    // Attempt 2
    await waitFor(() => screen.getByLabelText("Sun climbing"));
    fireEvent.click(screen.getByLabelText("Sun climbing"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));
    await waitFor(() => screen.getByRole("button", { name: /Continue/i }));
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    // Attempt 3: triggers offer_recap
    await waitFor(() => screen.getByLabelText("Sun climbing"));
    fireEvent.click(screen.getByLabelText("Sun climbing"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    // Should display recap action
    await waitFor(() => {
      expect(
        screen.getByRole("button", {
          name: /Review topic recap|Review taught concept|Recap slide/i,
        }),
      ).toBeDefined();
    });

    // Viewing recap displays taught slides
    fireEvent.click(
      screen.getByRole("button", {
        name: /Review topic recap|Review taught concept|Recap slide/i,
      }),
    );
    await waitFor(() => {
      expect(screen.getByText("Earth rotates eastward.")).toBeDefined();
    });

    // Recap must NOT resolve topic: Resolved 0 of 1
    expect(screen.getByText("Resolved 0 of 1")).toBeDefined();

    // Dismiss recap to continue test
    fireEvent.click(
      screen.getByRole("button", {
        name: /Return to test|Continue test|Done reviewing/i,
      }),
    );
    await waitFor(() => {
      expect(
        screen.getByText(/Retry variant · Same idea, fresh wording/i),
      ).toBeDefined();
    });
  });

  it("offers leave/resume after max_variants_per_test without marking test passed", async () => {
    const handleAnswer = vi.fn().mockResolvedValue({
      correct: false,
      answer_index: 1,
      explanation: "Try again",
      resolved_topics: 0,
      total_topics: 1,
      retry_queue_count: 1,
      passed: false,
      offer_leave_resume: true,
      retry_variant: v1Sunrise,
    });

    const onExit = vi.fn();
    render(
      <LessonTest
        lessonId="lesson-1"
        questions={[q1Original]}
        onPass={vi.fn()}
        onExit={onExit}
        onAnswer={handleAnswer}
      />,
    );

    fireEvent.click(screen.getByLabelText("Sun moves eastward"));
    fireEvent.click(screen.getByRole("button", { name: /Check/i }));

    await waitFor(() => {
      expect(
        screen.getByRole("button", {
          name: /Leave and resume later|Exit and resume later/i,
        }),
      ).toBeDefined();
    });

    fireEvent.click(
      screen.getByRole("button", {
        name: /Leave and resume later|Exit and resume later/i,
      }),
    );
    expect(onExit).toHaveBeenCalled();
  });
});
