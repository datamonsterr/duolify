import "./FormattedNote.css";
import { useUiPreferences } from "../design/localization/Preferences";
import type { QuickNoteItem } from "./NotesPage";

/** The AI-formatted title and body of a Voice note, with a switch back to the raw words. */
export function FormattedNote({
  note,
  showOriginal,
  onToggle,
}: {
  note: Pick<QuickNoteItem, "id" | "title" | "formatted_body">;
  showOriginal: boolean;
  onToggle: () => void;
}) {
  const { t } = useUiPreferences();
  if (!note.formatted_body) return null;
  return (
    <section
      className="note-formatted"
      data-testid="note-formatted"
      aria-label={t("notes.formatted_note")}
    >
      {note.title && <h3 className="note-formatted-title">{note.title}</h3>}
      <p className="note-formatted-body">{note.formatted_body}</p>
      <button
        type="button"
        className="note-formatted-toggle"
        data-testid="note-show-original"
        aria-pressed={showOriginal}
        onClick={onToggle}
      >
        {showOriginal ? t("notes.hide_original") : t("notes.show_original")}
      </button>
    </section>
  );
}
