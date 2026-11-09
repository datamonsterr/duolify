import { useEffect, useState } from "react";
import { Button } from "../design/Button";
import {
  OutlineEditor,
  type Outline,
  type OutlineSnapshot,
} from "../design/chat/OutlineEditor";
import {
  conflictFrom,
  outlineApi,
  validOutline,
  readWorkingOutline,
  writeWorkingOutline,
  type ConfirmedOutline,
  type OutlineConflict,
  type OutlineDraft,
  type OutlineService,
} from "./outline";

export function CourseOutline({
  briefRevision,
  initialValue,
  initialDraft,
  onBack,
  onSaved,
  onConfirmed,
  service = outlineApi,
}: {
  briefRevision: string;
  initialValue: Outline;
  initialDraft?: OutlineDraft;
  onBack: () => void;
  onSaved?: (draft: OutlineDraft) => void;
  onConfirmed?: (snapshot: ConfirmedOutline) => void;
  service?: OutlineService;
}) {
  const [working] = useState(() => readWorkingOutline(briefRevision));
  const [draft, setDraft] = useState(working ? working.draft : initialDraft);
  const [value, setValue] = useState(
    working?.value ?? initialDraft?.outline ?? initialValue,
  );
  const [history, setHistory] = useState<OutlineSnapshot[]>(
    working?.history ?? initialDraft?.history ?? [],
  );
  const [dirty, setDirty] = useState(!!working || !initialDraft);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const [confirmed, setConfirmed] = useState(
    working ? null : initialDraft?.confirmed,
  );
  const [conflict, setConflict] = useState<OutlineConflict>();
  const [epoch, setEpoch] = useState(0);
  useEffect(() => {
    writeWorkingOutline(
      briefRevision,
      dirty ? { value, history, draft } : undefined,
    );
  }, [briefRevision, dirty, value, history, draft]);
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
  async function persist(confirm = false) {
    if (busy || conflict || !validOutline(value, confirm)) return;
    setBusy(true);
    setError("");
    setStatus("");
    try {
      const saved = !draft
        ? await service.create(briefRevision, value, history)
        : dirty
          ? await service.save(draft, value, history)
          : draft;
      setDraft(saved);
      setDirty(false);
      onSaved?.(saved);
      setStatus("Outline saved.");
      if (confirm) {
        const snapshot = await service.confirm(saved);
        setConfirmed(snapshot);
        onConfirmed?.(snapshot);
        setStatus(
          "Outline confirmed. No course has been created. Creation needs a separate review and confirmation.",
        );
      }
    } catch (e) {
      const branches = conflictFrom(e);
      if (branches) {
        setConflict(branches);
        setError(
          "A newer outline is saved. Both drafts are kept. Choose which to continue.",
        );
      } else
        setError(
          e instanceof Error
            ? `${e.message} Your edits are kept.`
            : "Could not save. Your edits are kept; connect and retry.",
        );
    } finally {
      setBusy(false);
    }
  }
  async function recover(next: OutlineDraft) {
    if (busy) return;
    setBusy(true);
    try {
      const selected = await service.save(next, next.outline, next.history);
      setDraft(selected);
      setValue(selected.outline);
      setHistory(selected.history);
      setDirty(false);
      setConfirmed(selected.confirmed);
      setConflict(undefined);
      setError("");
      setStatus("Selected outline loaded. The other saved draft is kept.");
      setEpoch(epoch + 1);
      onSaved?.(selected);
    } catch (e) {
      const branches = conflictFrom(e);
      if (branches) setConflict(branches);
      setError("Could not select this draft. Both copies are kept; retry.");
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="course-outline" aria-label="Sample outline editor">
      <h2>Sample outline</h2>
      <p className="sample-badge">Sample · Manual edits and undo are free</p>
      <p>
        Your saved outline and the last 50 edits are available after reload.
        Confirming here saves your selection; it creates no course.
      </p>
      {error && <p role="alert">{error}</p>}
      {conflict && (
        <div aria-label="Recover outline drafts">
          {(
            [
              ["Newer saved outline", conflict.current],
              ["My recovery copy", conflict.recovery],
            ] as const
          ).map(([label, branch]) => (
            <div key={branch.id}>
              <details>
                <summary>
                  {label} · revision {branch.revision}
                </summary>
                <ol>
                  {branch.outline.map((unit) => (
                    <li key={unit.id}>
                      {unit.title}
                      <ol>
                        {unit.lessons.map((lesson) => (
                          <li key={lesson.id}>
                            {lesson.title}: {lesson.goal}
                          </li>
                        ))}
                      </ol>
                    </li>
                  ))}
                </ol>
              </details>
              <Button disabled={busy} onClick={() => void recover(branch)}>
                {label === "My recovery copy"
                  ? "Continue my recovery copy"
                  : "Use newer saved outline"}
              </Button>
            </div>
          ))}
        </div>
      )}
      <fieldset disabled={busy || !!conflict}>
        <legend className="guided-sr-only">
          Edit sample units and lessons
        </legend>
        <OutlineEditor
          key={epoch}
          persistent
          initialValue={value}
          initialHistory={history}
          onChange={(next, stack) => {
            setValue(next);
            setHistory(stack);
            setDirty(true);
            setConfirmed(null);
            setStatus("");
          }}
        />
        {!validOutline(value) && (
          <p role="alert">
            Enter a title and goal for every lesson and a title for every unit
            before saving.
          </p>
        )}
        <div className="outline-actions">
          <Button
            disabled={!dirty || !validOutline(value)}
            onClick={() => void persist()}
          >
            Save outline
          </Button>
          <Button
            disabled={!validOutline(value, true)}
            onClick={() => void persist(true)}
          >
            Confirm outline
          </Button>
          <Button variant="quiet" onClick={onBack}>
            Back to preview
          </Button>
        </div>
      </fieldset>
      <p role="status">
        {busy
          ? "Saving outline…"
          : status ||
            (dirty
              ? "Unsaved changes. Save your outline before leaving."
              : "Saved outline loaded.")}
      </p>
      {confirmed && (
        <p>
          Confirmed revision {confirmed.revision} · {confirmed.outline.length}{" "}
          units
        </p>
      )}
    </section>
  );
}
