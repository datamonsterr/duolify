import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { characterCreation, type CreateJob } from "./creation";
import type { CharacterRecord } from "./library";
import { session } from "../accounts/session";

const terminal = (j: CreateJob) =>
  ["succeeded", "failed", "rejected", "cancelled"].includes(j.state);
const message = (error: unknown) =>
  error instanceof Error ? error.message : "Connect and retry.";
export function CreateCharacter({
  record,
  onCreated,
}: {
  record?: CharacterRecord;
  onCreated: (record: CharacterRecord) => void;
}) {
  const [available, setAvailable] = useState<boolean | null>(null);
  const [prompt, setPrompt] = useState("");
  const [job, setJob] = useState<CreateJob | null>(null);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [retry, setRetry] = useState(0);
  const receive = useCallback((j: CreateJob) => {
    if (terminal(j)) setPrompt("");
    setJob(j);
  }, []);
  const key = useRef(crypto.randomUUID());
  const notified = useRef("");
  const heading = useRef<HTMLHeadingElement>(null);
  // Recovery contains an opaque job ID only. Prompts are memory-only and erased
  // on terminal state. Ownership is still enforced by every API read.
  const recoveryKey = `duolify.character-job.${session.current()?.id ?? "session"}`;
  const created = useRef(onCreated);
  useEffect(() => {
    created.current = onCreated;
  }, [onCreated]);
  useEffect(() => {
    let active = true;
    characterCreation
      .availability()
      .then((a) => {
        if (active) setAvailable(a.enabled);
      })
      .catch((e) => {
        if (active) {
          setAvailable(false);
          setError(message(e));
        }
      });
    const saved = sessionStorage.getItem(recoveryKey);
    if (saved)
      characterCreation
        .get(saved)
        .then((j) => {
          if (active) receive(j);
        })
        .catch(() => sessionStorage.removeItem(recoveryKey));
    return () => {
      active = false;
    };
  }, [recoveryKey, receive, retry]);
  useEffect(() => {
    if (!job) return;
    sessionStorage.setItem(recoveryKey, job.id);
    if (terminal(job)) {
      sessionStorage.removeItem(recoveryKey);
      if (job.character && notified.current !== job.id) {
        notified.current = job.id;
        created.current(job.character);
      }
      return;
    }
    if (!["queued", "running", "screening"].includes(job.state)) return;
    let active = true;
    const timer = setTimeout(() => {
      characterCreation
        .get(job.id)
        .then((j) => {
          if (active) {
            receive(j);
            setError("");
          }
        })
        .catch((e) => {
          if (active) setError(message(e));
        });
    }, 600);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [job, recoveryKey, retry, receive]);
  async function review() {
    if (pending || !available) return;
    setPending(true);
    setError("");
    try {
      receive(
        await characterCreation.quote({
          request_key: key.current,
          prompt,
          ...(record
            ? { character_id: record.id, expected_version: record.version }
            : {}),
        }),
      );
    } catch (e) {
      setError(message(e));
    } finally {
      setPending(false);
    }
  }
  async function confirm() {
    if (!job?.quote || pending) return;
    setPending(true);
    setError("");
    try {
      receive(await characterCreation.confirm(job.id, job.quote.id));
    } catch (e) {
      setError(message(e));
    } finally {
      setPending(false);
    }
  }
  const busy =
    pending ||
    (!!job && ["screening", "queued", "running"].includes(job.state));
  return (
    <section className="character-create character-card" aria-busy={busy}>
      <h2 ref={heading} tabIndex={-1}>
        {record ? "Regenerate private character" : "Create a private character"}
      </h2>
      <p>
        AI maps your description to the original rig, then screens it. Your
        description stays private. Manual edits and built-in characters are
        free.
      </p>
      {available === null && <p>Checking creation availability…</p>}
      {available === false && (
        <p>
          Creation is unavailable until pricing is approved. Built-ins and free
          edits remain available.
        </p>
      )}
      {error && (
        <div role="alert">
          <p>{error}</p>
          <Button variant="secondary" onClick={() => setRetry((n) => n + 1)}>
            Retry connection
          </Button>
          {error === "insufficient_credits" && (
            <a href="/credits">Review Credits</a>
          )}
        </div>
      )}
      {(!job || terminal(job)) && (
        <>
          {job?.state === "succeeded" && (
            <p role="status">
              Character ready. Previous lessons keep their saved version.
            </p>
          )}
          {job && job.state !== "succeeded" && (
            <p role="alert">
              {job.reason_code || job.state}. Your previous character is
              unchanged. No character credits were spent.
            </p>
          )}
          {(!job || !terminal(job)) && (
            <label>
              Describe your original character
              <textarea
                aria-label="Describe your original character"
                value={prompt}
                maxLength={3000}
                onChange={(e) => {
                  setPrompt(e.target.value);
                  key.current = crypto.randomUUID();
                }}
                disabled={busy || available !== true}
              />
              <small>1–1,500 characters. Original characters only.</small>
            </label>
          )}
          {job && terminal(job) ? (
            <Button
              variant="secondary"
              onClick={() => {
                key.current = crypto.randomUUID();
                setJob(null);
                setError("");
                heading.current?.focus();
              }}
            >
              New character request
            </Button>
          ) : (
            <Button
              disabled={available !== true || busy || !prompt.trim()}
              onClick={() => void review()}
            >
              Review character quote
            </Button>
          )}
        </>
      )}
      {job?.quote && !terminal(job) && (
        <div className="character-quote">
          {job.proposal && <strong>UNAPPROVED development proposal</strong>}
          <p>Pricing version: {job.quote.pricing_version}</p>
          <p>
            Maximum {job.quote.content_ceiling} Credits ·{" "}
            {record ? "Regeneration" : "Creation"}
          </p>
          {job.quote.expires_at && (
            <p>
              Quote expires: {new Date(job.quote.expires_at).toLocaleString()}
            </p>
          )}
          <p>
            Confirm to reserve Credits. A failed or cancelled result releases
            the reservation. Your existing version stays usable.
          </p>
          {job.state === "quoted" && (
            <Button disabled={busy} onClick={() => void confirm()}>
              Confirm character credits
            </Button>
          )}
        </div>
      )}
      {job && !terminal(job) && (
        <>
          {busy && (
            <p role="status">
              Creating and screening… You can reconnect to this job. The job has
              a bounded deadline.
            </p>
          )}
          <Button
            variant="secondary"
            onClick={() => {
              setError("");
              void characterCreation
                .cancel(job.id)
                .then(receive)
                .catch((e) => setError(message(e)));
            }}
          >
            Cancel character request
          </Button>
        </>
      )}
      <a className="shell-link" href="/courses">
        Use Default cast
      </a>
    </section>
  );
}
