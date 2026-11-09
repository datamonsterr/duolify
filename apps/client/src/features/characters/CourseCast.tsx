import { useEffect, useState } from "react";
import { Button } from "../design/Button";
import {
  characterLibrary,
  type CharacterRecord,
  type CastSelection,
} from "./library";
import { getDefaultCastFixtures } from "./fixtures";
import { CharacterPreview } from "./CharacterLibrary";

export function CourseCast({ courseId }: { courseId: string }) {
  const [records, setRecords] = useState<CharacterRecord[] | null>(null);
  const [selection, setSelection] = useState<CastSelection>({
    guide: "",
    supporting: [],
  });
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [pending, setPending] = useState(false);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let active = true;
    void Promise.all([characterLibrary.list(), characterLibrary.cast(courseId)])
      .then(([items, cast]) => {
        if (active) {
          setRecords(items);
          const exists = (id: string) =>
            [
              "default-guide",
              "default-supporter",
              "default-companion",
            ].includes(id) || items.some((record) => record.id === id);
          setSelection(
            cast.guide && !exists(cast.guide)
              ? { guide: "", supporting: [], version: cast.version }
              : { ...cast, supporting: cast.supporting.filter(exists) },
          );
          setError("");
        }
      })
      .catch((err) => {
        if (active)
          setError(err instanceof Error ? err.message : "Connect and retry.");
      });
    return () => {
      active = false;
    };
  }, [courseId, reload]);
  const builtIns = getDefaultCastFixtures().map((spec, index) => ({
    id: ["default-guide", "default-supporter", "default-companion"][index],
    version: 1,
    spec,
  }));
  const choices = [...builtIns, ...(records ?? [])];
  const keys = [selection.guide, ...selection.supporting];
  const distinct =
    keys.filter(Boolean).length === new Set(keys.filter(Boolean)).size;
  const selected = choices.find((r) => r.id === selection.guide) ?? builtIns[0];
  return (
    <section className="characters-page">
      <a className="shell-link" href={`/course/${courseId}`}>
        Back to Course
      </a>
      <h1>Course Cast</h1>
      <p>
        Choose one main guide and up to two distinct supporting characters.
        Changes apply to future lessons and chat.
      </p>
      <p>No character credits are spent.</p>
      <p>
        Provisional original art · published copies keep their published Cast
        until forked.
      </p>
      {error && (
        <div role="alert">
          <p>
            {error === "not_found"
              ? "This course cannot change its Cast. Use your owned course or the confirmed fork flow."
              : error}
          </p>
          <Button onClick={() => setReload((n) => n + 1)}>
            Retry connection
          </Button>
        </div>
      )}
      {notice && <p role="status">{notice}</p>}
      {records === null && !error && <p role="status">Loading Course Cast…</p>}
      {records && (
        <form
          className="character-editor"
          onSubmit={(e) => {
            e.preventDefault();
            setPending(true);
            setError("");
            void characterLibrary
              .select(courseId, selection)
              .then((value) => {
                setSelection(value);
                setNotice(
                  "Saved Course Cast. Previous lessons stay unchanged.",
                );
              })
              .catch((err) =>
                setError(
                  err instanceof Error ? err.message : "Connect and retry.",
                ),
              )
              .finally(() => setPending(false));
          }}
        >
          <CharacterPreview spec={selected.spec} />
          <label>
            Main guide
            <select
              value={
                choices.some((c) => c.id === selection.guide)
                  ? selection.guide
                  : ""
              }
              onChange={(e) => {
                setSelection({ guide: e.target.value, supporting: [] });
                setNotice("");
              }}
            >
              <option value="">Default cast</option>
              {choices.map((record) => (
                <option key={record.id} value={record.id}>
                  {record.spec.info.name}
                </option>
              ))}
            </select>
          </label>
          {[0, 1].map((index) => (
            <label key={index}>
              Supporting character {index + 1}
              <select
                disabled={!selection.guide}
                value={
                  choices.some((c) => c.id === selection.supporting[index])
                    ? selection.supporting[index]
                    : ""
                }
                onChange={(e) => {
                  const supporting = [...selection.supporting];
                  supporting[index] = e.target.value;
                  setSelection({
                    ...selection,
                    supporting: supporting.filter(Boolean),
                  });
                  setNotice("");
                }}
              >
                <option value="">None</option>
                {choices.map((record) => (
                  <option key={record.id} value={record.id}>
                    {record.spec.info.name}
                  </option>
                ))}
              </select>
            </label>
          ))}
          {!distinct && (
            <p role="alert">Choose distinct characters for each role.</p>
          )}
          <Button type="submit" pending={pending} disabled={!distinct}>
            Save Course Cast
          </Button>
          <a className="shell-link" href="/settings/characters">
            Edit private characters
          </a>
        </form>
      )}
    </section>
  );
}
