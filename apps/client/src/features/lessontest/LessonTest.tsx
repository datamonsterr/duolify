import { useState, useEffect, useCallback } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useShellSession } from "../shell/ShellSession";
import { version as appVersion } from "../../../package.json";
import { authRequest, AuthError } from "../accounts/session";
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

export interface RecapSlide {
  slide_id: string;
  html: string;
  duration_ms: number;
  narration?: string;
  topic_ids: string[];
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
  offer_recap?: boolean;
  recap_slides?: RecapSlide[];
  offer_leave_resume?: boolean;
  next_lesson_id?: string;
  next_lesson_status?: string;
  required_credits?: number;
  available_credits?: number;
}

export interface QueuedOfflineAnswer {
  operation_key: string;
  question_id: string;
  choice_index: number;
  choice_text?: string;
}

const queueStorageKey = (lessonId: string) =>
  `lessontest:sync-queue:${lessonId}`;

function isNetworkError(error: unknown): boolean {
  return (
    error instanceof Error &&
    !(error instanceof AuthError) &&
    /Failed to fetch|NetworkError|Network request failed|Load failed/i.test(
      error.message,
    )
  );
}

export function generateOperationKey(): string {
  if (
    typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
  ) {
    return crypto.randomUUID();
  }
  return (
    "op-" +
    Math.random().toString(36).slice(2, 11) +
    "-" +
    Date.now().toString(36)
  );
}

export function getOfflineAnswers(lessonId: string): QueuedOfflineAnswer[] {
  try {
    const raw = localStorage.getItem(queueStorageKey(lessonId));
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function enqueueOfflineAnswer(
  lessonId: string,
  item: QueuedOfflineAnswer,
) {
  try {
    const k = queueStorageKey(lessonId);
    const existing = getOfflineAnswers(lessonId);
    if (!existing.some((e) => e.operation_key === item.operation_key)) {
      existing.push(item);
      localStorage.setItem(k, JSON.stringify(existing));
    }
  } catch {
    // ignore storage failures
  }
}

export function clearOfflineAnswers(lessonId: string) {
  try {
    localStorage.removeItem(queueStorageKey(lessonId));
  } catch {
    // ignore storage failures
  }
}

export interface LessonTestProps {
  lessonId: string;
  courseId?: string;
  title?: string;
  questions: QuestionItem[];
  resolvedTopics?: number;
  totalTopics?: number;
  minGap?: number;
  passed?: boolean;
  onPass: () => void;
  onExit?: () => void;
  onAnswer?: (
    questionId: string,
    choiceIndex: number,
    operationKey?: string,
  ) => Promise<AnswerResult>;
}

export function LessonTest({
  lessonId,
  courseId,
  title = "Lesson test",
  questions: initialQuestions,
  resolvedTopics: initialResolvedTopics,
  totalTopics: initialTotalTopics,
  minGap = 2,
  passed = false,
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
  const [pendingOffline, setPendingOffline] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submissionError, setSubmissionError] = useState<string | null>(null);
  const [finished, setFinished] = useState(passed);
  const [resolvedTopics, setResolvedTopics] = useState(
    initialResolvedTopics ?? 0,
  );
  const [totalTopics, setTotalTopics] = useState(defaultTotalTopics);
  const [retryQueueCount, setRetryQueueCount] = useState(
    () => initialQuestions.filter((q) => q.is_variant || !!q.variant_of).length,
  );
  const [showRecap, setShowRecap] = useState(false);
  const [offlineAnswerCount, setOfflineAnswerCount] = useState(
    () => getOfflineAnswers(effectiveLessonId).length,
  );
  const [waitingToSync, setWaitingToSync] = useState(
    () => getOfflineAnswers(effectiveLessonId).length > 0,
  );

  const currentQuestion = queue[queueIndex];

  const applyResult = useCallback(
    (result: AnswerResult, questionId: string) => {
      if (typeof result.total_topics === "number")
        setTotalTopics(result.total_topics);
      if (typeof result.resolved_topics === "number")
        setResolvedTopics(result.resolved_topics);
      if (typeof result.retry_queue_count === "number")
        setRetryQueueCount(result.retry_queue_count);
      if (result.retry_variant) {
        const variant = result.retry_variant;
        setQueue((prev) => {
          if (prev.some((q) => q.question_id === variant.question_id))
            return prev;
          const sourceIndex = prev.findIndex(
            (q) => q.question_id === questionId,
          );
          const target = Math.min(
            prev.length,
            Math.max(queueIndex, sourceIndex + 1) + minGap,
          );
          const next = [...prev];
          next.splice(target, 0, variant);
          return next;
        });
      }
    },
    [minGap, queueIndex],
  );

  const handleReplaySync = useCallback(async () => {
    if (isSubmitting || !onAnswer) return;
    const queued = getOfflineAnswers(effectiveLessonId);
    if (queued.length === 0) return;
    setIsSubmitting(true);
    setSubmissionError(null);
    try {
      for (const item of queued) {
        const result =
          onAnswer.length >= 3
            ? await onAnswer(
                item.question_id,
                item.choice_index,
                item.operation_key,
              )
            : await onAnswer(item.question_id, item.choice_index);
        applyResult(result, item.question_id);
        // Remove only acknowledged operations so a partial replay is resumable.
        const remaining = getOfflineAnswers(effectiveLessonId).filter(
          (q) => q.operation_key !== item.operation_key,
        );
        localStorage.setItem(
          queueStorageKey(effectiveLessonId),
          JSON.stringify(remaining),
        );
        setOfflineAnswerCount(remaining.length);
        setWaitingToSync(false);
        setPendingOffline(false);
        if (currentQuestion?.question_id === item.question_id)
          setFeedback(result);
        if (result.passed) {
          setFeedback(result);
          setFinished(true);
          onPass();
          break;
        }
      }
    } catch (err) {
      if (!isNetworkError(err)) {
        setWaitingToSync(false);
        setSubmissionError(
          err instanceof Error ? err.message : "Could not sync answer",
        );
      }
    } finally {
      setIsSubmitting(false);
    }
  }, [
    isSubmitting,
    onAnswer,
    effectiveLessonId,
    onPass,
    applyResult,
    currentQuestion,
  ]);

  useEffect(() => {
    window.addEventListener("online", handleReplaySync);
    return () => window.removeEventListener("online", handleReplaySync);
  }, [handleReplaySync]);

  useEffect(() => {
    if (
      currentQuestion &&
      (currentQuestion.is_variant || Boolean(currentQuestion.variant_of))
    ) {
      track("ui_test_retry_presented", {
        lesson_id: effectiveLessonId,
        topic_id: currentQuestion.topic_id,
      });
    }
  }, [currentQuestion, effectiveLessonId]);

  const handleSelectChoice = useCallback(
    (idx: number) => {
      if (feedback !== null || pendingOffline) return;
      setSubmissionError(null);
      setSelectedChoice(idx);
    },
    [feedback, pendingOffline],
  );

  const handleCheck = useCallback(async () => {
    if (
      selectedChoice === null ||
      !currentQuestion ||
      isSubmitting ||
      pendingOffline
    )
      return;

    setIsSubmitting(true);
    setSubmissionError(null);
    const opKey = generateOperationKey();
    try {
      if (onAnswer) {
        try {
          const result =
            onAnswer.length >= 3
              ? await onAnswer(
                  currentQuestion.question_id,
                  selectedChoice,
                  opKey,
                )
              : await onAnswer(currentQuestion.question_id, selectedChoice);
          setFeedback(result);
          applyResult(result, currentQuestion.question_id);
        } catch (err) {
          if (!isNetworkError(err)) throw err;
          enqueueOfflineAnswer(effectiveLessonId, {
            operation_key: opKey,
            question_id: currentQuestion.question_id,
            choice_index: selectedChoice,
          });
          setOfflineAnswerCount(getOfflineAnswers(effectiveLessonId).length);
          if (queueIndex + 1 >= queue.length) setWaitingToSync(true);
          else setPendingOffline(true);
        }
      } else {
        // Fallback local grading if onAnswer not provided
        setResolvedTopics((prev) => Math.min(prev + 1, totalTopics));
        setFeedback({
          correct: true,
          passed: queueIndex + 1 >= queue.length,
        });
      }
    } catch (err: unknown) {
      const msg =
        err instanceof Error
          ? err.message
          : "Couldn't prepare the retry question";
      setSubmissionError(msg);
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
    effectiveLessonId,
    applyResult,
    pendingOffline,
  ]);

  const handleContinue = useCallback(() => {
    if (pendingOffline) {
      setPendingOffline(false);
      setSelectedChoice(null);
      setQueueIndex((prev) => prev + 1);
      return;
    }
    if (!feedback) return;

    if (feedback.passed) {
      setFinished(true);
      onPass();
      return;
    }

    setFeedback(null);
    setSelectedChoice(null);
    setShowRecap(false);
    setQueueIndex((prev) => prev + 1);
  }, [feedback, onPass, pendingOffline]);

  // Keyboard accessibility: 1-9 to select choice, Enter to Check/Continue
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (showRecap) {
        if (e.key === "Enter") {
          e.preventDefault();
          setShowRecap(false);
          handleContinue();
        }
        return;
      }

      if (feedback || pendingOffline) {
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
    showRecap,
    feedback,
    currentQuestion,
    selectedChoice,
    handleCheck,
    handleContinue,
    handleSelectChoice,
    pendingOffline,
  ]);

  if (waitingToSync) {
    return (
      <div
        className="finish-screen waiting-to-sync"
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
            background: "#FFF1D1",
            color: "#704400",
            boxShadow: "0 8px 0 #F1CD99",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinecap="round"
            strokeLinejoin="round"
            style={{ width: "60px", height: "60px" }}
          >
            <path d="M1 1l22 22M16.72 11.06A10.94 10.94 0 0 1 19 12.55M5 12.55a10.94 10.94 0 0 1 5.17-2.39M10.71 5.05A16 16 0 0 1 22.58 9M1.42 9a15.91 15.91 0 0 1 4.7-2.88M8.53 16.11a6 6 0 0 1 6.95 0M12 20h.01" />
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
          Waiting to sync
        </h2>
        <p
          style={{
            margin: 0,
            fontSize: "15px",
            lineHeight: 1.45,
            color: "#555",
            fontWeight: 600,
          }}
        >
          Your answers are saved. We'll verify your answers when you're back
          online before continuing the test.
        </p>

        <div
          className="ban bwarn"
          role="status"
          style={{
            width: "100%",
            display: "flex",
            alignItems: "flex-start",
            gap: "10px",
            padding: "12px 14px",
            borderRadius: "14px",
            backgroundColor: "#FFF7E6",
            border: "2px solid #FFD591",
            color: "#873800",
            textAlign: "left",
          }}
        >
          <div>
            <b
              style={{
                display: "block",
                fontSize: "14px",
                marginBottom: "2px",
              }}
            >
              Offline
            </b>
            <p style={{ margin: 0, fontSize: "13px", lineHeight: 1.35 }}>
              Saved lessons are still available. Progress will sync when
              reconnected.
            </p>
          </div>
        </div>

        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: "10px",
            width: "100%",
            marginTop: "16px",
          }}
        >
          <button
            type="button"
            className="btn"
            disabled={isSubmitting}
            onClick={handleReplaySync}
            style={{
              width: "100%",
              minHeight: "50px",
              borderRadius: "16px",
              backgroundColor: "#1F7A0D",
              color: "#fff",
              border: "none",
              fontWeight: 900,
              fontSize: "15px",
              textTransform: "uppercase",
              letterSpacing: "0.8px",
              cursor: "pointer",
            }}
          >
            {isSubmitting ? "Syncing..." : "Sync answers"}
          </button>
          <button
            type="button"
            className="btn sec"
            onClick={() => {
              if (onExit) onExit();
              else window.location.href = `/course/${effectiveCourseId}`;
            }}
            style={{
              width: "100%",
              minHeight: "44px",
              borderRadius: "14px",
              backgroundColor: "#fff",
              color: "#1667AB",
              border: "2px solid #D3D3D3",
              fontWeight: 800,
              fontSize: "14px",
              cursor: "pointer",
            }}
          >
            Back to learn path
          </button>
        </div>
      </div>
    );
  }

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
          Lesson complete!
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
            aria-label="Back to your path — Back to learn path"
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
            Back to your path
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
        <h1
          style={{
            margin: 0,
            fontSize: "14px",
            fontWeight: 700,
            color: "#666",
          }}
        >
          {title}
        </h1>
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

      {offlineAnswerCount > 0 && (
        <div
          className="ban bwarn"
          role="status"
          style={{
            marginBottom: "16px",
            padding: "12px 14px",
            borderRadius: "14px",
            backgroundColor: "#FFF7E6",
            border: "2px solid #FFD591",
            color: "#873800",
            display: "flex",
            alignItems: "center",
            gap: "10px",
          }}
        >
          <div>
            <b
              style={{
                display: "block",
                fontSize: "14px",
                marginBottom: "2px",
              }}
            >
              Offline variant
            </b>
            <p style={{ margin: 0, fontSize: "13px", lineHeight: 1.35 }}>
              {offlineAnswerCount}{" "}
              {offlineAnswerCount === 1 ? "answer is" : "answers are"} waiting
              to be checked. You'll see results and XP when you reconnect.
            </p>
          </div>
        </div>
      )}

      {showRecap &&
      feedback?.recap_slides &&
      feedback.recap_slides.length > 0 ? (
        <main>
          <h2
            style={{
              fontSize: "22px",
              fontWeight: 900,
              marginBottom: "16px",
              color: "#2B2B2B",
            }}
          >
            Topic recap
          </h2>
          <div
            className="recap-slides"
            style={{ display: "flex", flexDirection: "column", gap: "16px" }}
          >
            {feedback.recap_slides.map((s, idx) => (
              <div
                key={s.slide_id || idx}
                style={{
                  padding: "16px",
                  borderRadius: "16px",
                  border: "2px solid #E0E0E0",
                  backgroundColor: "#FAFAFA",
                }}
              >
                {s.html ? (
                  <div dangerouslySetInnerHTML={{ __html: s.html }} />
                ) : (
                  <p style={{ margin: 0 }}>{s.narration}</p>
                )}
                {s.narration && (!s.html || !s.html.includes(s.narration)) && (
                  <p
                    style={{
                      margin: "8px 0 0",
                      fontSize: "14px",
                      color: "#555",
                      fontStyle: "italic",
                    }}
                  >
                    {s.narration}
                  </p>
                )}
              </div>
            ))}
          </div>
        </main>
      ) : (
        <main>
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
                    borderWidth: "2px 2px 5px 2px",
                    borderStyle: "solid",
                    borderColor: borderColor,
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
                    disabled={feedback !== null || pendingOffline}
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
      )}

      <footer style={{ marginTop: "28px" }}>
        {showRecap ? (
          <button
            type="button"
            className="btn"
            onClick={() => {
              setShowRecap(false);
              handleContinue();
            }}
            style={{
              width: "100%",
              minHeight: "50px",
              borderRadius: "16px",
              backgroundColor: "#1F7A0D",
              color: "#fff",
              border: "none",
              fontWeight: 900,
              fontSize: "15px",
              textTransform: "uppercase",
              letterSpacing: "0.8px",
              cursor: "pointer",
            }}
          >
            Return to test
          </button>
        ) : (
          <>
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
                  style={{
                    fontSize: "18px",
                    fontWeight: 900,
                    marginBottom: "4px",
                  }}
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
                {feedback.offer_recap &&
                  feedback.recap_slides &&
                  feedback.recap_slides.length > 0 && (
                    <button
                      type="button"
                      className="btn sec"
                      onClick={() => {
                        setShowRecap(true);
                        track("lesson_test_recap_shown", {
                          lesson_id: effectiveLessonId,
                          topic_id: currentQuestion.topic_id,
                        });
                      }}
                      style={{
                        marginTop: "12px",
                        width: "100%",
                        minHeight: "44px",
                        borderRadius: "14px",
                        backgroundColor: "#fff",
                        color: "#1667AB",
                        border: "2px solid #8CCBF7",
                        fontWeight: 800,
                        fontSize: "14px",
                        cursor: "pointer",
                      }}
                    >
                      Review topic recap
                    </button>
                  )}
                {feedback.offer_leave_resume && (
                  <button
                    type="button"
                    className="btn sec"
                    onClick={() => {
                      if (onExit) onExit();
                    }}
                    style={{
                      marginTop: "12px",
                      width: "100%",
                      minHeight: "44px",
                      borderRadius: "14px",
                      backgroundColor: "#fff",
                      color: "#555",
                      border: "2px solid #D3D3D3",
                      fontWeight: 800,
                      fontSize: "14px",
                      cursor: "pointer",
                    }}
                  >
                    Leave and resume later
                  </button>
                )}
              </div>
            )}

            {submissionError ? (
              <div
                role="alert"
                style={{
                  borderRadius: "16px",
                  padding: "14px 16px",
                  display: "flex",
                  flexDirection: "column",
                  gap: "12px",
                  backgroundColor: "#FDECEC",
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
                    Couldn't prepare the retry question
                  </b>
                  <p
                    style={{
                      margin: 0,
                      fontSize: "13px",
                      lineHeight: "1.45",
                    }}
                  >
                    {submissionError}
                  </p>
                  <p>
                    Your progress and queue are saved. No credits are charged
                    for retries.
                  </p>
                </div>
                <div style={{ display: "flex", gap: "10px" }}>
                  <button
                    type="button"
                    className="btn primary"
                    onClick={handleCheck}
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      justifyContent: "center",
                      minHeight: "44px",
                      padding: "0 18px",
                      borderRadius: "14px",
                      border: "none",
                      background: "#1F7A0D",
                      color: "#fff",
                      fontWeight: 900,
                      fontSize: "14px",
                      cursor: "pointer",
                      textTransform: "uppercase",
                      letterSpacing: "0.8px",
                    }}
                  >
                    Retry
                  </button>
                  <button
                    type="button"
                    className="btn sec"
                    onClick={() => {
                      if (onExit) onExit();
                    }}
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      justifyContent: "center",
                      minHeight: "44px",
                      padding: "0 18px",
                      borderRadius: "14px",
                      border: "2px solid #D3D3D3",
                      background: "#fff",
                      color: "#1667AB",
                      fontWeight: 900,
                      fontSize: "14px",
                      cursor: "pointer",
                      textTransform: "uppercase",
                      letterSpacing: "0.8px",
                    }}
                  >
                    Return to lesson
                  </button>
                </div>
              </div>
            ) : pendingOffline ? (
              <button
                type="button"
                className="btn primary"
                onClick={handleContinue}
              >
                Continue
              </button>
            ) : feedback === null ? (
              <div
                style={{ display: "flex", flexDirection: "column", gap: "6px" }}
              >
                {isSubmitting && (
                  <div
                    role="status"
                    style={{
                      borderRadius: "14px",
                      padding: "10px 14px",
                      backgroundColor: "#DDF1FF",
                      color: "#0D5592",
                      fontSize: "13px",
                      fontWeight: 700,
                    }}
                  >
                    <b>Preparing a fresh question…</b> Writing a new version of
                    the idea.
                  </div>
                )}
                <button
                  type="button"
                  className="btn primary"
                  disabled={selectedChoice === null || isSubmitting}
                  onClick={handleCheck}
                  style={{
                    width: "100%",
                    minHeight: "50px",
                    borderRadius: "16px",
                    backgroundColor:
                      selectedChoice === null ? "#E5E5E5" : "#1F7A0D",
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
                {selectedChoice === null && (
                  <span
                    style={{
                      textAlign: "center",
                      fontSize: "13px",
                      fontWeight: 600,
                      color: "#5E5E5E",
                    }}
                  >
                    Select an answer to check it
                  </span>
                )}
              </div>
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
          </>
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
  const [minGap, setMinGap] = useState(2);
  const [passed, setPassed] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tracerEnabled) return;
    authRequest<{
      course_id?: string;
      questions: QuestionItem[];
      total_topics?: number;
      resolved_topics?: number;
      min_gap?: number;
      passed?: boolean;
    }>(`/v1/lessons/${lessonId}/test`, undefined, undefined, "GET", {
      "X-App-Version": appVersion,
    })
      .then((data) => {
        if (data.course_id) {
          setCourseId(data.course_id);
        }
        setQuestions(data.questions || []);
        setPassed(data.passed === true);
        if (typeof data.total_topics === "number") {
          setTotalTopics(data.total_topics);
        }
        if (typeof data.resolved_topics === "number") {
          setResolvedTopics(data.resolved_topics);
        }
        if (typeof data.min_gap === "number") {
          setMinGap(data.min_gap);
        }
        setLoading(false);
      })
      .catch((err) => {
        setError(err.message || "Failed to load lesson test");
        setLoading(false);
      });
  }, [lessonId, tracerEnabled]);

  const handleAnswer = async (
    questionId: string,
    choiceIndex: number,
    operationKey: string | undefined,
  ) => {
    const opKey = operationKey || generateOperationKey();
    const res = await authRequest<AnswerResult>(
      `/v1/lessons/${lessonId}/test/answer`,
      {
        question_id: questionId,
        choice_index: choiceIndex,
        operation_key: opKey,
      },
      undefined,
      "POST",
      { "X-App-Version": appVersion },
    );
    return res;
  };

  const handlePass = () => {
    // Completion analytics are owned by the committing API transaction.
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
      minGap={minGap}
      passed={passed}
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
