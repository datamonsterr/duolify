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
  variant_of?: string;
  is_variant?: boolean;
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
  retry_variant?: QuestionItem;
  next_lesson_id?: string;
  next_lesson_status?: string;
  required_credits?: number;
  available_credits?: number;
}

export interface LessonTestProps {
  lessonId: string;
  courseId?: string;
  title?: string;
  questions: QuestionItem[];
  resolvedTopics?: number;
  totalTopics?: number;
  onPass: () => void;
  onExit?: () => void;
  onAnswer?: (questionId: string, choiceIndex: number) => Promise<AnswerResult>;
}

export function LessonTest({
  lessonId,
  courseId,
  title = "Lesson test",
  questions: initialQuestions,
  resolvedTopics: initialResolvedTopics,
  totalTopics: initialTotalTopics,
  onPass,
  onExit,
  onAnswer,
}: LessonTestProps) {
  const session = useShellSession();
  const effectiveCourseId = courseId || session.courseId() || "astronomy";
  const effectiveLessonId = lessonId || "sky";

  const defaultTotalTopics =
    initialTotalTopics ??
    (new Set(initialQuestions.map((q) => q.topic_id || q.question_id)).size ||
      1);

  const [queue, setQueue] = useState<QuestionItem[]>(initialQuestions);
  const [queueIndex, setQueueIndex] = useState(0);
  const [selectedChoice, setSelectedChoice] = useState<number | null>(null);
  const [feedback, setFeedback] = useState<AnswerResult | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [finished, setFinished] = useState(false);
  const [resolvedTopics, setResolvedTopics] = useState(
    initialResolvedTopics ?? 0,
  );
  const [totalTopics, setTotalTopics] = useState(defaultTotalTopics);
  const [retryQueueCount, setRetryQueueCount] = useState(0);

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
        if (typeof result.total_topics === "number") {
          setTotalTopics(result.total_topics);
        }
        if (typeof result.resolved_topics === "number") {
          setResolvedTopics(result.resolved_topics);
        }
        if (typeof result.retry_queue_count === "number") {
          setRetryQueueCount(result.retry_queue_count);
        }
        if (!result.correct) {
          const variant: QuestionItem = result.retry_variant || {
            ...currentQuestion,
            question_id: `${currentQuestion.question_id}-retry`,
            variant_of: currentQuestion.question_id,
            is_variant: true,
            choices: currentQuestion.choices.map(
              (_, i) =>
                currentQuestion.choices[
                  (i + 1) % currentQuestion.choices.length
                ],
            ),
          };
          // Min gap rule: retry variant is queued after at least 2 remaining questions (or at end)
          setQueue((prev) => {
            const targetIdx = Math.min(prev.length, queueIndex + 1 + 2);
            const next = [...prev];
            next.splice(targetIdx, 0, variant);
            return next;
          });
        }
      } else {
        // Fallback local grading if onAnswer not provided
        setResolvedTopics((prev) => Math.min(prev + 1, totalTopics));
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
    totalTopics,
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
    const nextStatus = feedback?.next_lesson_status;
    const reqCredits = feedback?.required_credits ?? 190;
    const availCredits = feedback?.available_credits ?? 12;

    return (
      <div
        className="finish-screen"
        style={{
          maxWidth: "480px",
          margin: "0 auto",
          padding: "40px 20px 24px",
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          textAlign: "center",
          gap: "16px",
        }}
      >
        <div
          aria-hidden="true"
          style={{
            width: "120px",
            height: "120px",
            borderRadius: "50%",
            background: "#1F7A0D",
            boxShadow: "0 8px 0 #145208",
            color: "#fff",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="3"
            strokeLinecap="round"
            strokeLinejoin="round"
            style={{ width: "64px", height: "64px" }}
          >
            <path d="M5 12.5l4.5 4.5L19 7.5" />
          </svg>
        </div>

        <h2
          style={{
            margin: 0,
            fontSize: "30px",
            fontWeight: 900,
            color: "#2B2B2B",
            lineHeight: 1.1,
          }}
        >
          Lesson complete
        </h2>
        <p
          style={{
            margin: 0,
            fontSize: "15px",
            lineHeight: 1.5,
            color: "#555",
          }}
        >
          +15 XP · 8-day streak
        </p>

        {nextStatus === "insufficient_credits" && (
          <>
            <div
              className="ban bvi"
              style={{
                borderRadius: "16px",
                padding: "12px 14px",
                display: "flex",
                gap: "10px",
                alignItems: "flex-start",
                textAlign: "left",
                width: "100%",
                background: "#F0E7FF",
                color: "#5B2BB5",
              }}
            >
              <div>
                <b
                  style={{
                    display: "block",
                    fontSize: "15px",
                    fontWeight: 900,
                    marginBottom: "2px",
                  }}
                >
                  Next lesson needs {reqCredits} credits
                </b>
                <p style={{ margin: 0, fontSize: "13px", lineHeight: 1.45 }}>
                  Purchased balance: {availCredits}. Your progress is saved;
                  prepare it whenever you're ready.
                </p>
              </div>
            </div>
            <a
              href="/credits"
              className="btn vio"
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                width: "100%",
                minHeight: "50px",
                padding: "0 16px",
                border: "none",
                borderRadius: "16px",
                background: "#7B3FE4",
                boxShadow: "0 4px 0 #5A2BB0",
                color: "#fff",
                fontWeight: 900,
                fontSize: "15px",
                letterSpacing: ".8px",
                textTransform: "uppercase",
                textDecoration: "none",
                cursor: "pointer",
              }}
            >
              See credit options
            </a>
          </>
        )}

        {nextStatus === "failed" && (
          <>
            <div
              className="ban bbad"
              role="alert"
              style={{
                borderRadius: "16px",
                padding: "12px 14px",
                display: "flex",
                gap: "10px",
                alignItems: "flex-start",
                textAlign: "left",
                width: "100%",
                background: "#FDECEC",
                color: "#B3261E",
              }}
            >
              <div>
                <b
                  style={{
                    display: "block",
                    fontSize: "15px",
                    fontWeight: 900,
                    marginBottom: "2px",
                  }}
                >
                  Failure variant · next lesson couldn't be prepared
                </b>
                <p style={{ margin: 0, fontSize: "13px", lineHeight: 1.45 }}>
                  Reserved credits were released. This lesson is still complete.
                </p>
              </div>
            </div>
            <button
              type="button"
              className="btn sec"
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                width: "100%",
                minHeight: "50px",
                padding: "0 16px",
                border: "2px solid #D3D3D3",
                borderRadius: "16px",
                background: "#fff",
                boxShadow: "0 4px 0 #D3D3D3",
                color: "#1667AB",
                fontWeight: 900,
                fontSize: "15px",
                letterSpacing: ".8px",
                textTransform: "uppercase",
                cursor: "pointer",
              }}
            >
              Try again · new estimate
            </button>
          </>
        )}

        <div style={{ width: "100%", marginTop: "12px" }}>
          <a
            href={`/course/${effectiveCourseId}`}
            className="btn sec"
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              width: "100%",
              minHeight: "50px",
              padding: "0 16px",
              border: "2px solid #D3D3D3",
              borderRadius: "16px",
              background: "#fff",
              boxShadow: "0 4px 0 #D3D3D3",
              color: "#1667AB",
              fontWeight: 900,
              fontSize: "15px",
              letterSpacing: ".8px",
              textTransform: "uppercase",
              textDecoration: "none",
            }}
          >
            Back to learn path
          </a>
        </div>
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
        aria-valuemax={totalTopics}
        aria-valuenow={resolvedTopics}
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
            width: `${totalTopics > 0 ? (resolvedTopics / totalTopics) * 100 : 0}%`,
            backgroundColor: "#58C322",
            borderRadius: "999px",
            transition: "width 0.2s ease",
          }}
        />
      </div>

      <main>
        <div
          className="meta"
          style={{
            display: "flex",
            alignItems: "center",
            gap: "8px",
            marginBottom: "14px",
          }}
        >
          <span
            className="chip cok"
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: "6px",
              padding: "4px 9px",
              borderRadius: "12px",
              fontWeight: 800,
              fontSize: "12px",
              backgroundColor: "#E3F8D3",
              color: "#1E6E0C",
            }}
          >
            Resolved {resolvedTopics} of {totalTopics}
          </span>
          <span
            className="chip cinfo"
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: "6px",
              padding: "4px 9px",
              borderRadius: "12px",
              fontWeight: 800,
              fontSize: "12px",
              backgroundColor: "#DDF1FF",
              color: "#0D5592",
            }}
          >
            Retry queue {retryQueueCount}
          </span>
        </div>

        {(currentQuestion.is_variant ||
          Boolean(currentQuestion.variant_of)) && (
          <div style={{ marginBottom: "12px" }}>
            <span
              className="rv"
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: "6px",
                alignSelf: "flex-start",
                padding: "6px 10px",
                borderRadius: "12px",
                backgroundColor: "#F0E7FF",
                color: "#5B2BB5",
                fontWeight: 900,
                fontSize: "13px",
              }}
            >
              Retry variant · Same idea, fresh wording
            </span>
          </div>
        )}

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
  const [courseId, setCourseId] = useState<string | undefined>(undefined);
  const [questions, setQuestions] = useState<QuestionItem[]>([]);
  const [totalTopics, setTotalTopics] = useState<number | undefined>(undefined);
  const [resolvedTopics, setResolvedTopics] = useState<number | undefined>(
    undefined,
  );
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tracerEnabled) return;
    track("lesson_test_started", { lesson_id: lessonId });
    authRequest<{
      course_id?: string;
      questions: QuestionItem[];
      total_topics?: number;
      resolved_topics?: number;
    }>(`/v1/lessons/${lessonId}/test`)
      .then((data) => {
        if (data.course_id) {
          setCourseId(data.course_id);
        }
        setQuestions(data.questions || []);
        if (typeof data.total_topics === "number") {
          setTotalTopics(data.total_topics);
        }
        if (typeof data.resolved_topics === "number") {
          setResolvedTopics(data.resolved_topics);
        }
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

  const effectiveCourseId = courseId || session.courseId() || "astronomy";

  if (error) {
    return (
      <div style={{ padding: "32px", textAlign: "center" }}>
        <p>{error}</p>
        <Link to="/course/$courseId" params={{ courseId: effectiveCourseId }}>
          Exit test
        </Link>
      </div>
    );
  }

  return (
    <LessonTest
      key={questions.map((q) => q.question_id).join(",") || lessonId}
      lessonId={lessonId}
      courseId={effectiveCourseId}
      questions={questions}
      totalTopics={totalTopics}
      resolvedTopics={resolvedTopics}
      onAnswer={handleAnswer}
      onPass={handlePass}
      onExit={() =>
        navigate({
          to: "/course/$courseId",
          params: { courseId: effectiveCourseId },
        })
      }
    />
  );
}
