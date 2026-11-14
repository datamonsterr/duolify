import { useCallback, useEffect, useId, useState } from "react";
import {
  MAX_CHANGELOG,
  republish,
  republishFailure,
  republishMessages,
  type RepublishFailure,
  type Republished,
  type Versions,
} from "./republish";

/** Owner controls to publish a new immutable version, inside the Share panel. */
export function RepublishPanel({ courseId }: { courseId: string }) {
  const id = useId();
  const [versions, setVersions] = useState<Versions | null>(null);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<Republished | null>(null);
  const [failure, setFailure] = useState<RepublishFailure | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [from, setFrom] = useState(1);

  const load = useCallback(() => {
    let active = true;
    republish
      .versions(courseId)
      .then((value) => active && (setVersions(value), setFailure(null)))
      .catch((error: unknown) => active && setFailure(republishFailure(error)))
      .finally(() => active && setLoaded(true));
    return () => {
      active = false;
    };
  }, [courseId]);
  useEffect(() => load(), [load]);

  const live = versions?.versions.find((v) => v.live)?.number ?? 0;
  const left = versions?.republishes_remaining ?? 0;

  async function publish() {
    setBusy(true);
    setFailure(null);
    setResult(null);
    setFrom(live || 1);
    try {
      const got = await republish.publish(courseId, text);
      setResult(got);
      if (got.verdict === "pass") setText("");
      setVersions(await republish.versions(courseId).catch(() => versions));
    } catch (error) {
      setFailure(republishFailure(error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section
      className="sharing-republish"
      aria-label="Publish new version"
      data-testid="republish-panel"
    >
      <h3>New version</h3>
      {!loaded && (
        <p role="status">
          <span className="sharing-spinner" aria-hidden="true" />
          Loading…
        </p>
      )}
      {failure && (
        <div className="sharing-error" role="alert">
          <p>{republishMessages[failure]}</p>
          {failure === "offline" && !versions && (
            <button type="button" className="duo-button" onClick={load}>
              Retry
            </button>
          )}
        </div>
      )}
      {versions && (
        <>
          <label htmlFor={`${id}-log`}>What's new in this version</label>
          <textarea
            id={`${id}-log`}
            className="sharing-textarea"
            value={text}
            maxLength={MAX_CHANGELOG}
            rows={3}
            disabled={busy}
            onChange={(event) => setText(event.target.value)}
          />
          <p className="sharing-note">
            {text.length} / {MAX_CHANGELOG}
          </p>
          <p className="sharing-note">
            {left} of {versions.limit} new versions left today. Each version is
            checked for safety before it goes live.
          </p>
          <button
            type="button"
            className="duo-button"
            disabled={busy || left <= 0}
            onClick={() => void publish()}
          >
            Publish new version
          </button>
        </>
      )}
      {busy && (
        <p role="status">
          <span className="sharing-spinner" aria-hidden="true" />
          Checking this version for safety…
        </p>
      )}
      {result?.verdict === "pass" && (
        <div className="sharing-banner ok" role="status">
          <b>Version {result.number} is live</b>
          <p>
            Learners on version {from} see “Update available” with your
            changelog.
          </p>
        </div>
      )}
      {result?.verdict === "pending_screening" && (
        <div className="sharing-banner" role="status">
          <b>Checking version {result.number} for safety</b>
          <p>
            Usually a few minutes. Version {live || 1} stays live meanwhile.
          </p>
        </div>
      )}
      {result?.verdict === "needs_review" && (
        <div className="sharing-banner" role="status">
          <b>Needs a quick review</b>
          <p>
            A moderator will look within 48 hours. Version {live || 1} stays
            live.
          </p>
        </div>
      )}
      {result?.verdict === "block" && (
        <div className="sharing-banner" role="alert">
          <b>This version can't be published</b>
          <p>
            Remove the flagged content and publish again. Version {live || 1}{" "}
            stays live.
          </p>
        </div>
      )}
    </section>
  );
}
