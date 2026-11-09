import { useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { characterLibrary, type CharacterRecord } from "./library";
import type { CharacterSpec, Accessory, Expression } from "./types";
import { compileRigSvg } from "./rig";
import { validateCharacterSpec } from "./schema";
import "./characters.css";

function failure(error: unknown) {
  return error instanceof Error ? error.message : "Connect and retry.";
}

export function CharacterPreview({ spec }: { spec: CharacterSpec }) {
  const [expression, setExpression] = useState<Expression>("idle");
  const svg = compileRigSvg(spec, expression);
  return (
    <div className="character-preview">
      <iframe
        title="Character preview"
        sandbox=""
        srcDoc={`<!doctype html><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"><style>body{margin:0;display:grid;place-items:center}svg{width:160px;height:160px}</style>${svg}`}
      />
      <label>
        Expression
        <select
          value={expression}
          onChange={(e) => setExpression(e.target.value as Expression)}
        >
          {spec.expressions.map((value) => (
            <option key={value}>{value}</option>
          ))}
        </select>
      </label>
      <small>Provisional original rig · static preview</small>
    </div>
  );
}

export function CharacterLibrary() {
  const [records, setRecords] = useState<CharacterRecord[] | null>(null);
  const [selected, setSelected] = useState<CharacterRecord | null>(null);
  const [draft, setDraft] = useState<CharacterSpec | null>(null);
  const [deleting, setDeleting] = useState<CharacterRecord | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [pending, setPending] = useState(false);
  const [reload, setReload] = useState(0);
  const title = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    let active = true;
    characterLibrary
      .list()
      .then((value) => {
        if (active) {
          setRecords(value);
          setError("");
        }
      })
      .catch((err) => {
        if (active) setError(failure(err));
      });
    return () => {
      active = false;
    };
  }, [reload]);
  function close() {
    setSelected(null);
    setDraft(null);
    setError("");
    setTimeout(() => title.current?.focus(), 0);
  }
  async function save() {
    if (!draft || !selected || pending) return;
    if (!validateCharacterSpec(draft).valid) {
      setError(
        "invalid_spec: Check field lengths, unique traits/accessories and palette contrast.",
      );
      return;
    }
    setPending(true);
    setError("");
    try {
      const updated = await characterLibrary.edit(selected, draft);
      setRecords((current) =>
        current!.map((record) => (record.id === updated.id ? updated : record)),
      );
      close();
      setNotice("Saved a new version. Previous lessons stay unchanged.");
    } catch (err) {
      setError(failure(err));
    } finally {
      setPending(false);
    }
  }
  return (
    <section className="characters-page" aria-busy={pending}>
      <a className="shell-link" href="/settings">
        Back to Settings
      </a>
      <h1 ref={title} tabIndex={-1}>
        Private characters
      </h1>
      <p>
        Your private library. Manual edits are free. New versions apply to
        future lessons and chat.
      </p>
      <p>
        Original fixture art is provisional. Prompt creation is unavailable
        pending approval.
      </p>
      {notice && <p role="status">{notice}</p>}
      {error && (
        <div role="alert">
          <p>{error}</p>
          <Button variant="secondary" onClick={() => setReload((n) => n + 1)}>
            Retry connection
          </Button>
          {error === "version_conflict" && selected && (
            <Button
              variant="secondary"
              onClick={() => {
                const latest = records?.find(
                  (record) => record.id === selected.id,
                );
                if (latest) {
                  setSelected(latest);
                  setDraft(structuredClone(latest.spec));
                  setError("");
                }
              }}
            >
              Load latest version
            </Button>
          )}
        </div>
      )}
      {records === null && !error && <p role="status">Loading characters…</p>}
      {!selected && records && (
        <div className="character-list">
          {records.length === 0 && <p>No private characters yet.</p>}
          {records.map((record) => (
            <article className="character-card" key={record.id}>
              <h2>{record.spec.info.name}</h2>
              <p>{record.spec.info.bio}</p>
              <small>Version {record.version} · Private</small>
              <CharacterPreview spec={record.spec} />
              <Button
                onClick={() => {
                  setSelected(record);
                  setDraft(structuredClone(record.spec));
                  setNotice("");
                  setError("");
                }}
              >
                Edit {record.spec.info.name}
              </Button>
              <Button variant="quiet" onClick={() => setDeleting(record)}>
                Delete {record.spec.info.name}
              </Button>
            </article>
          ))}
        </div>
      )}
      {selected && draft && (
        <form
          className="character-editor"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <h2>Edit {selected.spec.info.name}</h2>
          <CharacterPreview spec={draft} />
          <fieldset disabled={pending}>
            <legend>Info and personality</legend>
            {(
              [
                ["name", "Name", 40],
                ["bio", "Bio", 280],
                ["backstory", "Backstory", 800],
              ] as const
            ).map(([key, label, max]) => (
              <label key={key}>
                {label}
                <textarea
                  aria-label={label}
                  autoFocus={key === "name"}
                  value={draft.info[key]}
                  maxLength={max * 2}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      info: { ...draft.info, [key]: e.target.value },
                    })
                  }
                />
              </label>
            ))}
            <label>
              Traits
              <input
                value={draft.personality.traits.join(", ")}
                onChange={(e) =>
                  setDraft({
                    ...draft,
                    personality: {
                      ...draft.personality,
                      traits: e.target.value
                        .split(",")
                        .map((value) => value.trim()),
                    },
                  })
                }
              />
              <small>
                1–5 unique traits, separated by commas; 40 characters each.
              </small>
            </label>
            {(
              [
                ["speaking_style", "Speaking style", 200],
                ["persona_prompt", "Persona prompt", 500],
              ] as const
            ).map(([key, label, max]) => (
              <label key={key}>
                {label}
                <textarea
                  aria-label={label}
                  maxLength={max * 2}
                  value={draft.personality[key]}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      personality: {
                        ...draft.personality,
                        [key]: e.target.value,
                      },
                    })
                  }
                />
              </label>
            ))}
          </fieldset>
          <fieldset disabled={pending}>
            <legend>Original appearance and voice</legend>
            {(
              [
                ["body_shape", "Body shape", ["round", "bean", "star"]],
                ["eyes", "Eyes", ["round", "oval", "sleepy"]],
                ["mouth", "Mouth", ["smile", "line", "open"]],
              ] as const
            ).map(([key, label, options]) => (
              <label key={key}>
                {label}
                <select
                  value={draft.appearance[key]}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      appearance: {
                        ...draft.appearance,
                        [key]: e.target.value,
                      },
                    })
                  }
                >
                  {options.map((value) => (
                    <option key={value}>{value}</option>
                  ))}
                </select>
              </label>
            ))}
            {(["body", "accent", "outline"] as const).map((key) => (
              <label key={key}>
                {key[0].toUpperCase() + key.slice(1)} color
                <input
                  value={draft.appearance.palette[key]}
                  pattern="#[0-9A-Fa-f]{6}"
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      appearance: {
                        ...draft.appearance,
                        palette: {
                          ...draft.appearance.palette,
                          [key]: e.target.value,
                        },
                      },
                    })
                  }
                />
              </label>
            ))}
            <div className="character-accessories">
              {(["glasses", "scarf", "hat", "badge"] as Accessory[]).map(
                (value) => (
                  <label key={value}>
                    <input
                      type="checkbox"
                      checked={draft.appearance.accessories.includes(value)}
                      onChange={(e) =>
                        setDraft({
                          ...draft,
                          appearance: {
                            ...draft.appearance,
                            accessories: e.target.checked
                              ? [...draft.appearance.accessories, value]
                              : draft.appearance.accessories.filter(
                                  (a) => a !== value,
                                ),
                          },
                        })
                      }
                    />
                    {value}
                  </label>
                ),
              )}
            </div>
            <small>
              Choose up to two accessories. Body and outline need readable
              contrast.
            </small>
            <label>
              Voice
              <select
                value={draft.voice_key}
                onChange={(e) =>
                  setDraft({ ...draft, voice_key: e.target.value })
                }
              >
                {["guide", "supporter", "companion"].map((key) => (
                  <option key={key} value={`voice-provisional-${key}`}>
                    Provisional {key}
                  </option>
                ))}
              </select>
            </label>
          </fieldset>
          <div className="character-actions">
            <Button type="submit" pending={pending}>
              Save free edit
            </Button>
            <Button variant="secondary" disabled={pending} onClick={close}>
              Cancel
            </Button>
          </div>
        </form>
      )}
      {deleting && (
        <div className="settings-scrim">
          <div
            className="settings-dialog"
            role="dialog"
            aria-modal="true"
            aria-label="Delete character"
            onKeyDown={(e) => {
              if (e.key === "Escape" && !pending) setDeleting(null);
              if (e.key === "Tab") {
                const buttons =
                  e.currentTarget.querySelectorAll<HTMLButtonElement>("button");
                if (
                  !e.shiftKey &&
                  document.activeElement === buttons[buttons.length - 1]
                ) {
                  e.preventDefault();
                  buttons[0].focus();
                }
                if (e.shiftKey && document.activeElement === buttons[0]) {
                  e.preventDefault();
                  buttons[buttons.length - 1].focus();
                }
              }
            }}
          >
            <h2>Delete {deleting.spec.info.name}?</h2>
            {error && <p role="alert">{error}</p>}
            <p>
              Future choices use a safe default. Previous lessons retain their
              saved version.
            </p>
            <Button
              autoFocus
              pending={pending}
              onClick={() => {
                setPending(true);
                void characterLibrary
                  .delete(deleting.id)
                  .then(() => {
                    setRecords((current) =>
                      current!.filter((r) => r.id !== deleting.id),
                    );
                    setDeleting(null);
                    title.current?.focus();
                  })
                  .catch((err) => setError(failure(err)))
                  .finally(() => setPending(false));
              }}
            >
              Confirm deletion
            </Button>
            <Button
              disabled={pending}
              variant="secondary"
              onClick={() => {
                setDeleting(null);
                title.current?.focus();
              }}
            >
              Cancel
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}
