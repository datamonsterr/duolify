import { useEffect, useRef, useState, type ReactNode } from "react";
import { Button } from "../design/Button";
import { ForkWarning } from "./ForkWarning";
import {
  instructionApi,
  type Instruction,
  type InstructionList,
  type InstructionService,
} from "./api";
import {
  DEFAULT_LIMITS,
  countChars,
  deliveryHint,
  headerCopy,
  reasonCopy,
  scopeLabel,
  statusLabel,
} from "./copy";
import { useMobile } from "./useMobile";
import "./steering.css";

type Draft = {
  id: string;
  version: number;
  text: string;
  scope: string;
  busy: boolean;
  error: string;
};
const message = (e: unknown) =>
  e instanceof Error ? e.message : "screening_unavailable";
function recount(d: InstructionList, instructions: Instruction[]) {
  const active = instructions.filter((i) => i.status === "active");
  return {
    ...d,
    instructions,
    active_count: active.length,
    active_characters: active.reduce((n, i) => n + countChars(i.text), 0),
  };
}

export function InstructionPanel({
  courseId,
  service = instructionApi,
}: {
  courseId: string;
  service?: InstructionService;
}) {
  const mobile = useMobile();
  const root = useRef<HTMLElement>(null);
  const pending = useRef<string | null>(null);
  const [tick, setTick] = useState(0);
  const [data, setData] = useState<InstructionList | null>(null);
  const [text, setText] = useState("");
  const [scope, setScope] = useState("next_lesson");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState(false);
  const [epoch, setEpoch] = useState(0);
  const [notice, setNotice] = useState("");
  const [screen, setScreen] = useState<"list" | "add">("list");
  const [edit, setEdit] = useState<Draft | null>(null);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [warnFork, setWarnFork] = useState(false);

  const limits = data?.limits ?? DEFAULT_LIMITS;
  const max = limits.max_characters;
  const count = countChars(text);
  const records = data ? [...data.instructions].reverse() : [];

  function focusLater(key: string) {
    pending.current = key;
    setTick((n) => n + 1);
  }
  useEffect(() => {
    const key = pending.current;
    if (!key) return;
    pending.current = null;
    [...(root.current?.querySelectorAll<HTMLElement>("[data-focus]") ?? [])]
      .find((n) => n.dataset.focus === key)
      ?.focus();
  }, [tick]);
  useEffect(() => {
    let active = true;
    service
      .list(courseId)
      .then((r) => {
        if (active) {
          setData(r);
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

  async function refresh() {
    try {
      const r = await service.list(courseId);
      setData(r);
      return r;
    } catch {
      return null;
    }
  }
  async function save(confirmed = false) {
    if (busy || count === 0 || count > max) return;
    // First instruction on a published copy: explain the fork and ask before anything is sent.
    if (!confirmed && data?.fork?.required) {
      setWarnFork(true);
      return;
    }
    setWarnFork(false);
    setBusy(true);
    setError("");
    try {
      const r = await service.add(
        courseId,
        confirmed ? { text, scope, confirm_fork: true } : { text, scope },
      );
      setData((d) =>
        d
          ? {
              ...recount(d, [...d.instructions, r]),
              fork: d.fork
                ? { ...d.fork, required: false, forked: true }
                : d.fork,
            }
          : d,
      );
      setText("");
      setNotice("Instruction saved.");
      if (mobile) {
        setScreen("list");
        focusLater("add-button");
      }
    } catch (e) {
      if (e instanceof Error && e.message === "fork_confirmation_required")
        setWarnFork(true);
      else setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  function declineFork() {
    setWarnFork(false);
    focusLater("save");
  }
  function openEdit(i: Instruction) {
    setNotice("");
    setConfirmId(null);
    setEdit({
      id: i.id,
      version: i.version ?? 0,
      text: i.text,
      scope: i.scope,
      busy: false,
      error: "",
    });
    focusLater("edit-field");
  }
  function closeEdit(id: string) {
    setEdit(null);
    focusLater(`edit:${id}`);
  }
  async function saveEdit() {
    if (!edit || edit.busy) return;
    const d = edit;
    const n = countChars(d.text);
    if (n === 0 || n > max || !d.text.trim()) return;
    setEdit({ ...d, busy: true, error: "" });
    try {
      const r = await service.edit(courseId, d.id, {
        expected_version: d.version,
        text: d.text,
        scope: d.scope,
      });
      setData((x) =>
        x
          ? recount(
              x,
              x.instructions.map((i) => (i.id === r.id ? r : i)),
            )
          : x,
      );
      setEdit(null);
      setNotice("Instruction saved.");
      focusLater(`edit:${d.id}`);
    } catch (e) {
      const code = message(e);
      let version = d.version;
      if (["version_conflict", "not_found", "not_editable"].includes(code)) {
        const fresh = await refresh();
        version =
          fresh?.instructions.find((i) => i.id === d.id)?.version ?? version;
      }
      setEdit((cur) =>
        cur && cur.id === d.id
          ? { ...cur, busy: false, error: code, version }
          : cur,
      );
    }
  }
  async function remove(id: string) {
    if (deleting) return;
    setDeleting(true);
    setDeleteError("");
    try {
      await service.remove(courseId, id);
      setData((d) =>
        d
          ? recount(
              d,
              d.instructions.filter((i) => i.id !== id),
            )
          : d,
      );
      setConfirmId(null);
      setNotice("Instruction deleted. Past lessons are unchanged.");
      focusLater("list-heading");
    } catch (e) {
      const code = message(e);
      setDeleteError(code);
      if (code === "not_found") void refresh();
    } finally {
      setDeleting(false);
    }
  }
  function cancelConfirm() {
    const id = confirmId;
    setConfirmId(null);
    setDeleteError("");
    if (id) focusLater(`delete:${id}`);
  }
  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key !== "Escape") return;
    const stop = () => {
      e.preventDefault();
      e.stopPropagation();
    };
    if (confirmId) {
      stop();
      cancelConfirm();
    } else if (edit && !edit.busy) {
      stop();
      closeEdit(edit.id);
    } else if (mobile && screen === "add" && !busy) {
      stop();
      setScreen("list");
      focusLater("add-button");
    }
  }

  const editForm = edit && (
    <form
      className="steering-edit"
      onSubmit={(e) => {
        e.preventDefault();
        void saveEdit();
      }}
    >
      <label className="steering-field">
        Edit instruction
        <textarea
          data-focus="edit-field"
          value={edit.text}
          rows={4}
          disabled={edit.busy}
          aria-describedby="steering-edit-help"
          onChange={(e) =>
            setEdit({ ...edit, text: e.target.value, error: "" })
          }
        />
      </label>
      <p id="steering-edit-help">
        {countChars(edit.text)} / {max} · Edits change future lessons only. Past
        lessons never change.
      </p>
      <fieldset disabled={edit.busy}>
        <legend>Change scope</legend>
        {["next_lesson", "rest_of_course"].map((s) => (
          <label key={s}>
            <input
              type="radio"
              name="steering-edit-scope"
              checked={edit.scope === s}
              onChange={() => setEdit({ ...edit, scope: s })}
            />
            {scopeLabel(s)}
          </label>
        ))}
      </fieldset>
      {edit.error && (
        <Problem title="This change could not be saved" code={edit.error} />
      )}
      <div className="steering-actions">
        <Button
          type="submit"
          disabled={
            edit.busy || !edit.text.trim() || countChars(edit.text) > max
          }
        >
          {edit.busy
            ? "Screening…"
            : edit.error
              ? "Retry saving"
              : "Save changes"}
        </Button>
        <Button
          variant="secondary"
          disabled={edit.busy}
          onClick={() => closeEdit(edit.id)}
        >
          Cancel
        </Button>
        {mobile && (
          <Button
            variant="quiet"
            disabled={edit.busy}
            onClick={() => closeEdit(edit.id)}
          >
            Back
          </Button>
        )}
      </div>
    </form>
  );

  const limitsNote = (
    <p>
      Screening is free. Limits: {limits.max_active} active instructions,{" "}
      {limits.max_total_characters.toLocaleString("en-US")} characters total,{" "}
      {limits.adds_per_hour} adds per course per hour.
    </p>
  );
  const addForm = (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <label className="steering-field">
        New instruction
        <textarea
          data-focus="new-field"
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
        {count} / {max} · Style and focus only. URLs are not allowed.
      </p>
      <fieldset disabled={busy}>
        <legend>Apply to</legend>
        {["next_lesson", "rest_of_course"].map((s) => (
          <label key={s}>
            <input
              type="radio"
              name="steering-scope"
              checked={scope === s}
              onChange={() => setScope(s)}
            />
            {scopeLabel(s)}
          </label>
        ))}
      </fieldset>
      {!mobile && limitsNote}
      <Button
        type="submit"
        data-focus="save"
        disabled={busy || loading || loadError || !text.trim() || count > max}
      >
        {busy
          ? "Screening…"
          : error && !loadError
            ? "Retry saving"
            : "Save instruction"}
      </Button>
    </form>
  );

  const alert = error && (
    <div role="alert">
      <b>
        {loadError
          ? "Instruction unavailable"
          : "This instruction could not be saved"}
      </b>
      <p>{error}</p>
      {!loadError && reasonCopy(error) && <p>{reasonCopy(error)}</p>}
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
  );

  const showList = !mobile || screen === "list";
  const editingHere = mobile && edit;
  return (
    <section
      ref={root}
      className="steering-panel"
      aria-label="Course instructions"
      aria-busy={loading || busy}
      onKeyDown={onKeyDown}
    >
      <p
        role="status"
        aria-live="polite"
        className={loading || notice ? "" : "steering-live-empty"}
      >
        {loading ? "Loading instruction…" : notice}
      </p>
      {editingHere ? (
        editForm
      ) : showList ? (
        <>
          <p>
            Tell the course what you want more or less of. Newer instructions
            win. Past lessons never change.
          </p>
          <p>
            A lesson job already running keeps its instructions. Your new
            instruction applies to the next job that has not started.
          </p>
          {data && (
            <h3 tabIndex={-1} data-focus="list-heading">
              {headerCopy(
                data.active_count,
                data.active_characters,
                limits.max_total_characters,
              )}
            </h3>
          )}
          {data && data.active_count > 1 && (
            <p>
              When two active instructions conflict, the newest instruction
              wins.
            </p>
          )}
          {data && records.length === 0 && !loadError && (
            <p>No instruction yet.</p>
          )}
          {records.length > 0 && (
            <ul className="steering-list">
              {records.map((i) => {
                const hint = deliveryHint(i);
                const active = i.status === "active";
                const open = edit?.id === i.id;
                return (
                  <li key={i.id} className="steering-record">
                    {open ? (
                      editForm
                    ) : (
                      <>
                        <p dir="auto" id={`steering-text-${i.id}`}>
                          {i.text}
                        </p>
                        <div className="steering-meta">
                          <b>{statusLabel(i)}</b>
                          <p>{scopeLabel(i.scope)}</p>
                        </div>
                        {hint && <p className="steering-hint">{hint}</p>}
                        {active && (
                          <div className="steering-actions">
                            <Button
                              variant="secondary"
                              data-focus={`edit:${i.id}`}
                              aria-describedby={`steering-text-${i.id}`}
                              disabled={!!edit || deleting}
                              onClick={() => openEdit(i)}
                            >
                              Edit
                            </Button>
                            <Button
                              variant="secondary"
                              data-focus={`delete:${i.id}`}
                              aria-describedby={`steering-text-${i.id}`}
                              disabled={!!edit || confirmId === i.id}
                              onClick={() => {
                                setNotice("");
                                setDeleteError("");
                                setConfirmId(i.id);
                                focusLater(`keep:${i.id}`);
                              }}
                            >
                              Delete
                            </Button>
                          </div>
                        )}
                        {confirmId === i.id && (
                          <div
                            className="steering-confirm-box"
                            role="group"
                            aria-label="Confirm delete"
                          >
                            <p className="steering-confirm">
                              Delete this instruction? Past lessons never
                              change, and a lesson already being prepared keeps
                              the instructions it started with.
                            </p>
                            {deleteError && (
                              <Problem
                                title="This instruction could not be deleted"
                                code={deleteError}
                              />
                            )}
                            <div className="steering-actions">
                              <Button
                                variant="secondary"
                                data-focus={`keep:${i.id}`}
                                disabled={deleting}
                                onClick={cancelConfirm}
                              >
                                Keep
                              </Button>
                              <Button
                                pending={deleting}
                                onClick={() => void remove(i.id)}
                              >
                                Delete instruction
                              </Button>
                            </div>
                          </div>
                        )}
                      </>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
          {alert}
          {mobile ? (
            <>
              {limitsNote}
              <Button
                data-focus="add-button"
                disabled={loading || loadError}
                onClick={() => {
                  setScreen("add");
                  setNotice("");
                  focusLater("new-field");
                }}
              >
                Add instruction
              </Button>
            </>
          ) : (
            addForm
          )}
        </>
      ) : (
        <>
          <div className="steering-actions">
            <Button
              variant="quiet"
              disabled={busy}
              onClick={() => {
                setScreen("list");
                if (!loadError) setError("");
                focusLater("add-button");
              }}
            >
              Back
            </Button>
          </div>
          <h3>Add instruction</h3>
          {alert}
          {addForm}
        </>
      )}
      {warnFork && (
        <ForkWarning
          keptLessons={data?.fork?.kept_lessons ?? 0}
          lessonCredits={data?.fork?.lesson_credits}
          onCancel={declineFork}
          onConfirm={() => void save(true)}
        />
      )}
    </section>
  );
}

function Problem({ title, code }: { title: string; code: string }): ReactNode {
  return (
    <div role="alert">
      <b>{title}</b>
      <p>{code}</p>
      {reasonCopy(code) && <p>{reasonCopy(code)}</p>}
    </div>
  );
}
