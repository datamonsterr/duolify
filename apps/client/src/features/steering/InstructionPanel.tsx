import { useEffect, useState } from "react";
import { Button } from "../design/Button";
import {
  instructionApi,
  type Instruction,
  type InstructionService,
} from "./api";
import "./steering.css";

export function InstructionPanel({
  courseId,
  service = instructionApi,
}: {
  courseId: string;
  service?: InstructionService;
}) {
  const [instruction, setInstruction] = useState<Instruction | null>(null);
  const [text, setText] = useState("");
  const [scope, setScope] = useState("next_lesson");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState(false);
  const [epoch, setEpoch] = useState(0);
  useEffect(() => {
    let active = true;
    service
      .latest(courseId)
      .then((r) => {
        if (active) {
          setInstruction(r);
          setLoading(false);
          setLoadError(false);
          setError("");
        }
      })
      .catch(() => {
        if (active) {
          setLoading(false);
          setLoadError(true);
          setError("Could not load. Connect and retry loading.");
        }
      });
    return () => {
      active = false;
    };
  }, [courseId, service, epoch]);
  const count = Array.from(text).length;
  async function save() {
    if (busy || count === 0 || count > 500) return;
    setBusy(true);
    setError("");
    try {
      const r = await service.add(courseId, { text, scope });
      setInstruction(r);
      setText("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "screening_unavailable");
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      className="steering-panel"
      aria-label="Course instructions"
      aria-busy={loading || busy}
    >
      <p>
        Tell the course what you want more or less of. Newer instructions win.
        Past lessons never change.
      </p>
      <p>
        A lesson job already running keeps its instructions. Your new
        instruction applies to the next job that has not started.
      </p>
      {loading ? (
        <p role="status">Loading instruction…</p>
      ) : instruction ? (
        <article className="steering-record">
          <p dir="auto">{instruction.text}</p>
          <b>
            {instruction.status === "applied"
              ? `Applied to lesson ${instruction.applied_lesson_id}`
              : "Active"}
          </b>
          <p>
            {instruction.scope === "next_lesson"
              ? "Next lesson"
              : "Rest of course"}
          </p>
        </article>
      ) : (
        !loadError && <p>No instruction yet.</p>
      )}
      {error && (
        <div role="alert">
          <b>
            {loadError
              ? "Instruction unavailable"
              : "This instruction could not be saved"}
          </b>
          <p>{error}</p>
          <p>
            Your draft is kept. Instructions change style and focus; grading and
            prices stay the same.
          </p>
          {loadError && (
            <Button
              onClick={() => {
                setLoading(true);
                setEpoch((n) => n + 1);
              }}
            >
              Retry loading
            </Button>
          )}
        </div>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <label className="steering-field">
          New instruction
          <textarea
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              if (!loadError) setError("");
            }}
            rows={4}
            disabled={busy}
            aria-describedby="steering-help"
          />
        </label>
        <p id="steering-help">
          {count} / 500 · Style and focus only. URLs are not allowed.
        </p>
        <fieldset disabled={busy}>
          <legend>Apply to</legend>
          <label>
            <input
              type="radio"
              name="steering-scope"
              checked={scope === "next_lesson"}
              onChange={() => setScope("next_lesson")}
            />
            Next lesson
          </label>
          <label>
            <input
              type="radio"
              name="steering-scope"
              checked={scope === "rest_of_course"}
              onChange={() => setScope("rest_of_course")}
            />
            Rest of course
          </label>
        </fieldset>
        <p>
          Screening is free. Limits: 20 active instructions, 4,000 characters
          total, 10 adds per course per hour.
        </p>
        <Button
          type="submit"
          disabled={busy || loading || loadError || !text.trim() || count > 500}
        >
          {busy
            ? "Screening…"
            : error && !loadError
              ? "Retry saving"
              : "Save instruction"}
        </Button>
      </form>
    </section>
  );
}
