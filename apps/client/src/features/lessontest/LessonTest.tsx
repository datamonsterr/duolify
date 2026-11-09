import { useState, useEffect, useCallback } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useShellSession } from "../shell/ShellSession";
import { authRequest } from "../accounts/session";
import { track } from "../../lib/analytics";
import { RouteUnavailable } from "../shell/RouteBoundary";
import { useLessonTestTracerEnabled } from "../../lib/features";

export interface QuestionItem {
  question_id: string;
  topic_id: string;
  prompt: string;
  choices: string[];
}

export interface AnswerResult {
  correct: boolean;
  answer_index?: number;
  explanation?: string;
  resolved_topics?: number;
  total_topics?: number;
  retry_queue_count?: number;
  passed: boolean;
}

export interface LessonTestProps {
  lessonId: string;
  courseId?: string;
  title?: string;
  questions: QuestionItem[];
  onPass: () => void;
  onExit?: () => void;
  onAnswer?: (questionId: string, choiceIndex: number) => Promise<AnswerResult>;
}

export function LessonTest({
  lessonId,
  courseId,
  title = "Lesson test",
  questions: initialQuestions,
  onPass,
  onExit,
  onAnswer,
}: LessonTestProps) {
  const session = useShellSession();
  const effectiveCourseId = courseId || session.courseId() || "astronomy";
  const effectiveLessonId = lessonId || "sky";

  const [queue, setQueue] = useState<QuestionItem[]>(initialQuestions);
  const [queueIndex, setQueueIndex] = useState(0);
  const [selectedChoice, setSelectedChoice] = useState<number | null>(null);
  const [feedback, setFeedback] = useState<AnswerResult | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [finished, setFinished] = useState(false);

  const currentQuestion = queue[queueIndex];

  const handleSelectChoice = useCallback(
    (idx: number) => {
      if (feedback !== null) return;
      setSelectedChoice(idx);
    },
    [feedback],
  );

  const handleCheck = useCallback(async () => {
    if (selectedChoice === null || !currentQuestion || isSubmitting) return;

    setIsSubmitting(true);
    try {
      if (onAnswer) {
        const result = await onAnswer(
          currentQuestion.question_id,
          selectedChoice,
        );
        setFeedback(result);
        if (!result.correct) {
          // Reorder choices and append fallback to the retry queue
          const n = currentQuestion.choices.length;
          const reorderedChoices = currentQuestion.choices.map(
            (_, i) => currentQuestion.choices[(i + 1) % n],
          );
          setQueue((prev) => [
            ...prev,
            {
              ...currentQuestion,
              choices: reorderedChoices,
            },
          ]);
        }
      } else {
        // Fallback local grading if onAnswer not provided
        setFeedback({
          correct: true,
          passed: queueIndex + 1 >= queue.length,
        });
      }
    } finally {
      setIsSubmitting(false);
    }
  }, [
    selectedChoice,
    currentQuestion,
    isSubmitting,
    onAnswer,
    queueIndex,
    queue.length,
  ]);

  const handleContinue = useCallback(() => {
    if (!feedback) return;

    if (feedback.passed) {
      setFinished(true);
      onPass();
      return;
    }

    setFeedback(null);
    setSelectedChoice(null);
    setQueueIndex((prev) => prev + 1);
  }, [feedback, onPass]);

  // Keyboard accessibility: 1-9 to select choice, Enter to Check/Continue
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (feedback) {
        if (e.key === "Enter") {
          e.preventDefault();
          handleContinue();
        }
        return;
      }

      if (
        currentQuestion &&
        e.key >= "1" &&
        e.key <= String(currentQuestion.choices.length)
      ) {
        const choiceIdx = parseInt(e.key, 10) - 1;
        if (choiceIdx >= 0 && choiceIdx < currentQuestion.choices.length) {
          e.preventDefault();
          handleSelectChoice(choiceIdx);
        }
      } else if (e.key === "Enter" && selectedChoice !== null) {
        e.preventDefault();
        handleCheck();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [
    feedback,
    currentQuestion,
    selectedChoice,
    handleCheck,
    handleContinue,
    handleSelectChoice,
  ]);

  if (finished) {
    return (
      <div
        className="complete"
        style={{ padding: "40px 20px", textAlign: "center" }}
      >
        <div
          className="big-star"
          style={{ fontSize: "48px", marginBottom: "16px" }}
        >
          ✦
        </div>
        <h1 style={{ fontSize: "28px", fontWeight: 900, marginBottom: "8px" }}>
          Lesson complete!
        </h1>
        <p style={{ color: "#555", fontSize: "16px", marginBottom: "24px" }}>
          Wonderful work. Your universe just got a little bigger.
        </p>
        <a
          href={`/course/${effectiveCourseId}`}
          className="btn"
          style={{
            display: "inline-block",
            padding: "14px 28px",
            background: "#1F7A0D",
            color: "#fff",
            textDecoration: "none",
            borderRadius: "16px",
            fontWeight: 900,
          }}
        >
          Back to your path
        </a>
      </div>
    );
  }

  if (!currentQuestion) {
    return (
      <div style={{ padding: "40px 20px", textAlign: "center" }}>
        <p>Loading questions…</p>
      </div>
    );
  }

  return (
    <div
      className="lesson-test"
      data-lesson-id={effectiveLessonId}
      style={{ maxWidth: "600px", margin: "0 auto", padding: "20px 16px" }}
    >
      <header
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: "16px",
        }}
      >
        <a
          href={`/course/${effectiveCourseId}`}
          className="shell-link"
          onClick={(e) => {
            if (onExit) {
              e.preventDefault();
              onExit();
            }
          }}
          style={{ textDecoration: "none", color: "#1667AB", fontWeight: 800 }}
        >
          Exit test
        </a>
        <span style={{ fontSize: "14px", fontWeight: 700, color: "#666" }}>
          {title}
        </span>
      </header>

      <div
        className="progress"
        role="progressbar"
        aria-label="Test progress"
        aria-valuemin={0}
        aria-valuemax={queue.length}
        aria-valuenow={queueIndex + 1}
        style={{
          height: "12px",
          backgroundColor: "#E5E5E5",
          borderRadius: "999px",
          overflow: "hidden",
          marginBottom: "24px",
        }}
      >
        <div
          style={{
            height: "100%",
            width: `${((queueIndex + 1) / queue.length) * 100}%`,
            backgroundColor: "#58C322",
            borderRadius: "999px",
            transition: "width 0.2s ease",
          }}
        />
      </div>

      <main>
        <h2
          style={{
            fontSize: "22px",
            fontWeight: 900,
            marginBottom: "20px",
            color: "#2B2B2B",
          }}
        >
          {currentQuestion.prompt}
        </h2>

        <fieldset
          style={{
            border: "none",
            margin: 0,
            padding: 0,
            display: "flex",
            flexDirection: "column",
            gap: "12px",
          }}
        >
          <legend style={{ position: "absolute", left: "-9999px" }}>
            Question choices
          </legend>
          {currentQuestion.choices.map((choice, idx) => {
            const isSelected = selectedChoice === idx;
            const isWrong = feedback && !feedback.correct && isSelected;
            const isGood = feedback && feedback.correct && isSelected;

            let borderColor = "#E0E0E0";
            let bgColor = "#fff";
            let color = "#2B2B2B";

            if (isSelected) {
              borderColor = "#8CCBF7";
              bgColor = "#DDF1FF";
              color = "#0D5592";
            }
            if (isGood) {
              borderColor = "#7DCB55";
              bgColor = "#E3F8D3";
              color = "#1E6E0C";
            } else if (isWrong) {
              borderColor = "#E39B9B";
              bgColor = "#FDECEC";
              color = "#B3261E";
            }

            return (
              <label
                key={choice}
                onClick={() => handleSelectChoice(idx)}
                style={{
                  position: "relative",
                  display: "flex",
                  alignItems: "center",
                  gap: "12px",
                  padding: "14px 16px",
                  border: `2px solid ${borderColor}`,
                  borderBottomWidth: "5px",
                  borderRadius: "16px",
                  backgroundColor: bgColor,
                  color,
                  fontSize: "16px",
                  fontWeight: 700,
                  cursor: feedback ? "default" : "pointer",
                  userSelect: "none",
                }}
              >
                <input
                  type="radio"
                  name="choice"
                  value={choice}
                  checked={isSelected}
                  disabled={feedback !== null}
                  onChange={() => handleSelectChoice(idx)}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    height: "100%",
                    opacity: 0,
                    margin: 0,
                    cursor: feedback ? "default" : "pointer",
                  }}
                  aria-label={choice}
                />
                <span
                  style={{
                    width: "28px",
                    height: "28px",
                    borderRadius: "8px",
                    border: "2px solid #D3D3D3",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    fontSize: "13px",
                    fontWeight: 900,
                    color: isSelected ? "#0D5592" : "#555",
                    borderColor: isSelected ? "#1677C9" : "#D3D3D3",
                    flexShrink: 0,
                    pointerEvents: "none",
                  }}
                >
                  {idx + 1}
                </span>
                <span style={{ pointerEvents: "none" }}>{choice}</span>
              </label>
            );
          })}
        </fieldset>
      </main>

      <footer style={{ marginTop: "28px" }}>
        {feedback && (
          <div
            role="status"
            style={{
              padding: "16px",
              borderRadius: "16px",
              marginBottom: "16px",
              backgroundColor: feedback.correct ? "#E3F8D3" : "#FDECEC",
              color: feedback.correct ? "#1E6E0C" : "#B3261E",
            }}
          >
            <div
              style={{ fontSize: "18px", fontWeight: 900, marginBottom: "4px" }}
            >
              {feedback.correct ? "Correct!" : "Not quite — here's why"}
            </div>
            {feedback.explanation && (
              <p
                style={{
                  margin: 0,
                  fontSize: "14px",
                  lineHeight: 1.45,
                  fontWeight: 600,
                }}
              >
                {feedback.correct
                  ? feedback.explanation.replace(/^Correct!?\s*/i, "")
                  : feedback.explanation}
              </p>
            )}
          </div>
        )}

        {feedback === null ? (
          <button
            type="button"
            className="btn primary"
            disabled={selectedChoice === null || isSubmitting}
            onClick={handleCheck}
            style={{
              width: "100%",
              minHeight: "50px",
              borderRadius: "16px",
              backgroundColor: selectedChoice === null ? "#E5E5E5" : "#1F7A0D",
              color: selectedChoice === null ? "#999" : "#fff",
              border: "none",
              fontWeight: 900,
              fontSize: "15px",
              textTransform: "uppercase",
              letterSpacing: "0.8px",
              cursor: selectedChoice === null ? "not-allowed" : "pointer",
            }}
          >
            Check
          </button>
        ) : (
          <button
            type="button"
            className="btn"
            onClick={handleContinue}
            style={{
              width: "100%",
              minHeight: "50px",
              borderRadius: "16px",
              backgroundColor: feedback.correct ? "#1F7A0D" : "#C62828",
              color: "#fff",
              border: "none",
              fontWeight: 900,
              fontSize: "15px",
              textTransform: "uppercase",
              letterSpacing: "0.8px",
              cursor: "pointer",
            }}
          >
            Continue
          </button>
        )}
      </footer>
    </div>
  );
}

export function LessonTestRoute() {
  const { lessonId } = useParams({ from: "/lesson/$lessonId/test" });
  const navigate = useNavigate();
  const session = useShellSession();
  const tracerEnabled = useLessonTestTracerEnabled();
  const [questions, setQuestions] = useState<QuestionItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tracerEnabled) return;
    track("lesson_test_started", { lesson_id: lessonId });
    authRequest<{ questions: QuestionItem[] }>(`/v1/lessons/${lessonId}/test`)
      .then((data) => {
        setQuestions(data.questions || []);
        setLoading(false);
      })
      .catch((err) => {
        setError(err.message || "Failed to load lesson test");
        setLoading(false);
      });
  }, [lessonId, tracerEnabled]);

  const handleAnswer = async (questionId: string, choiceIndex: number) => {
    track("lesson_test_submitted", {
      lesson_id: lessonId,
      choice_index: choiceIndex,
    });
    const currentQ = questions.find((q) => q.question_id === questionId);
    const choiceText = currentQ ? currentQ.choices[choiceIndex] : undefined;
    const res = await authRequest<AnswerResult>(
      `/v1/lessons/${lessonId}/test/answer`,
      {
        question_id: questionId,
        choice_index: choiceIndex,
        ...(choiceText ? { choice_text: choiceText } : {}),
      },
    );
    return res;
  };

  const handlePass = () => {
    track("lesson_test_passed", { lesson_id: lessonId });
  };

  if (!tracerEnabled) {
    return <RouteUnavailable />;
  }

  if (loading) {
    return (
      <p role="status" style={{ padding: "32px", textAlign: "center" }}>
        Loading test questions…
      </p>
    );
  }

  if (error) {
    return (
      <div style={{ padding: "32px", textAlign: "center" }}>
        <p>{error}</p>
        <Link to="/course/$courseId" params={{ courseId: session.courseId() }}>
          Exit test
        </Link>
      </div>
    );
  }

  return (
    <LessonTest
      key={questions.map((q) => q.question_id).join(",") || lessonId}
      lessonId={lessonId}
      courseId={session.courseId()}
      questions={questions}
      onAnswer={handleAnswer}
      onPass={handlePass}
      onExit={() =>
        navigate({
          to: "/course/$courseId",
          params: { courseId: session.courseId() },
        })
      }
    />
  );
}
