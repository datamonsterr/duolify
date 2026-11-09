import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { session } from "../accounts/session";
import { useMobileLayout } from "../shell/AdaptiveLayouts";
import { noteQueue, type QueuedNote } from "./note-queue";

export type QuickNoteItem = {
  id: string;
  learner_id?: string;
  action_key: string;
  course_id?: string | null;
  lesson_id?: string | null;
  slide_id?: number | null;
  slide_title?: string | null;
  source: "slide" | "lesson" | "chat";
  raw_text: string;
  status:
    "enrichment_pending" | "enriched" | "enrichment_failed" | "waiting_to_sync";
  summary?: string | null;
  tags?: string[] | null;
  reviewed_at?: string | null;
  created_at: string;
  updated_at: string;
};

export function NotesPage() {
  const [notes, setNotes] = useState<QuickNoteItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchQuery, setSearchQuery] = useState("");
  const [filterNotReviewed, setFilterNotReviewed] = useState(false);
  const [selectedNoteId, setSelectedNoteId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function loadNotes() {
      const queuedItems: QuickNoteItem[] = noteQueue
        .getQueue()
        .map((item: QueuedNote) => ({
          id: item.serverNoteId ?? item.actionKey,
          action_key: item.actionKey,
          course_id: item.courseId,
          lesson_id: item.lessonId,
          slide_id: item.slideId,
          slide_title: item.slideTitle,
          source: item.source,
          raw_text: item.rawText,
          status: item.serverNoteId ? "enrichment_pending" : "waiting_to_sync",
          created_at: new Date(item.createdAt).toISOString(),
          updated_at: new Date(item.createdAt).toISOString(),
        }));

      try {
        const fetched = await session.request<
          { notes: QuickNoteItem[] } | QuickNoteItem[]
        >("/v1/notes");
        if (cancelled) return;
        const serverNotes = Array.isArray(fetched)
          ? fetched
          : (fetched?.notes ?? []);

        // Merge server notes with unsynced local queued items
        const serverActionKeys = new Set(serverNotes.map((n) => n.action_key));
        const unsyncedQueued = queuedItems.filter(
          (q) => !serverActionKeys.has(q.action_key),
        );

        const combined = [...unsyncedQueued, ...serverNotes];
        setNotes(combined);
        if (combined.length > 0 && !selectedNoteId) {
          setSelectedNoteId(combined[0].id);
        }
      } catch {
        if (cancelled) return;
        // On offline or error, display local queued notes
        setNotes(queuedItems);
        if (queuedItems.length > 0 && !selectedNoteId) {
          setSelectedNoteId(queuedItems[0].id);
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    void loadNotes();
    return () => {
      cancelled = true;
    };
  }, [selectedNoteId]);

  const filteredNotes = notes.filter((n) => {
    if (filterNotReviewed && n.reviewed_at) return false;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      const matchText = n.raw_text.toLowerCase().includes(q);
      const matchTitle = n.slide_title?.toLowerCase().includes(q) ?? false;
      if (!matchText && !matchTitle) return false;
    }
    return true;
  });

  const selectedNote =
    notes.find((n) => n.id === selectedNoteId) ?? filteredNotes[0] ?? null;
  const isMobile = useMobileLayout();

  return (
    <div
      className="notes-container"
      style={{
        display: "flex",
        flexDirection: isMobile ? "column" : "row",
        flex: 1,
        minHeight: "100%",
        width: "100%",
      }}
    >
      {/* Sidebar List */}
      <div
        className="notes-sidebar"
        style={{
          width: "100%",
          maxWidth: isMobile ? "100%" : "420px",
          borderRight: isMobile ? "none" : "2px solid #EFEFEF",
          borderBottom: isMobile ? "2px solid #EFEFEF" : "none",
          padding: isMobile ? "16px" : "24px",
          display: "flex",
          flexDirection: "column",
          gap: "12px",
        }}
      >
        <h2
          style={{
            fontSize: "26px",
            margin: 0,
            fontWeight: 900,
            color: "#2B2B2B",
          }}
        >
          Quick notes
        </h2>

        <label
          style={{
            display: "flex",
            alignItems: "center",
            gap: "8px",
            border: "2px solid #CFCFCF",
            background: "#F7F7F7",
            borderRadius: "14px",
            padding: "9px 12px",
          }}
        >
          <span style={{ position: "absolute", left: "-9999px" }}>
            Search notes
          </span>
          <input
            style={{
              border: "none",
              background: "none",
              font: "inherit",
              flex: 1,
              outline: "none",
            }}
            placeholder="Search your notes"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
        </label>

        <div style={{ display: "flex", gap: "6px", flexWrap: "wrap" }}>
          <button
            type="button"
            className="duo-button quiet"
            style={{
              borderRadius: "12px",
              padding: "6px 12px",
              fontWeight: 800,
              fontSize: "13px",
              border: "2px solid",
              borderColor: !filterNotReviewed ? "#8CCBF7" : "#E0E0E0",
              background: !filterNotReviewed ? "#DDF1FF" : "#fff",
              color: !filterNotReviewed ? "#0D5592" : "#2B2B2B",
            }}
            aria-pressed={!filterNotReviewed}
            onClick={() => setFilterNotReviewed(false)}
          >
            All
          </button>
          <button
            type="button"
            className="duo-button quiet"
            style={{
              borderRadius: "12px",
              padding: "6px 12px",
              fontWeight: 800,
              fontSize: "13px",
              border: "2px solid",
              borderColor: filterNotReviewed ? "#8CCBF7" : "#E0E0E0",
              background: filterNotReviewed ? "#DDF1FF" : "#fff",
              color: filterNotReviewed ? "#0D5592" : "#2B2B2B",
            }}
            aria-pressed={filterNotReviewed}
            onClick={() => setFilterNotReviewed(!filterNotReviewed)}
          >
            Not reviewed
          </button>
        </div>

        <span
          style={{ fontSize: "13px", fontWeight: 600, color: "#5E5E5E" }}
          role="status"
        >
          {filteredNotes.length} {filteredNotes.length === 1 ? "note" : "notes"}
          {(searchQuery || filterNotReviewed) && (
            <>
              {" · "}
              <button
                type="button"
                style={{
                  background: "none",
                  border: "none",
                  padding: 0,
                  color: "#1667AB",
                  cursor: "pointer",
                  fontWeight: 800,
                }}
                onClick={() => {
                  setSearchQuery("");
                  setFilterNotReviewed(false);
                }}
              >
                Clear filters
              </button>
            </>
          )}
        </span>

        {/* Empty State */}
        {!loading && notes.length === 0 && (
          <div
            style={{
              textAlign: "center",
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              gap: "12px",
              padding: "40px 16px",
            }}
          >
            <div
              style={{
                width: "80px",
                height: "80px",
                borderRadius: "24px",
                background: "#DDF1FF",
                color: "#1677C9",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                fontSize: "32px",
              }}
            >
              📝
            </div>
            <h2
              style={{
                fontSize: "20px",
                fontWeight: 900,
                color: "#2B2B2B",
                margin: 0,
              }}
            >
              Keep an idea from your next lesson
            </h2>
            <p
              style={{
                margin: 0,
                fontSize: "14px",
                color: "#555",
                lineHeight: 1.5,
              }}
            >
              Tap Add note in any slide. Your words are saved even without
              credits.
            </p>
            <Link
              to="/courses"
              className="duo-button primary"
              style={{
                minHeight: "44px",
                padding: "0 20px",
                fontSize: "14px",
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              Go to lesson
            </Link>
          </div>
        )}

        {/* Note Cards List */}
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: "8px",
            overflowY: "auto",
          }}
        >
          {filteredNotes.map((note) => {
            const isSelected = selectedNote?.id === note.id;
            return (
              <button
                key={note.id}
                type="button"
                onClick={() => setSelectedNoteId(note.id)}
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: "6px",
                  padding: "12px 14px",
                  borderRadius: "18px",
                  border: "2px solid",
                  borderColor: isSelected ? "#8CCBF7" : "#E3E3E3",
                  backgroundColor: isSelected ? "#F4FAFF" : "#fff",
                  borderBottomWidth: "5px",
                  textAlign: "left",
                  cursor: "pointer",
                }}
              >
                <b
                  style={{
                    fontSize: "15px",
                    fontWeight: 900,
                    color: "#2B2B2B",
                  }}
                >
                  {note.raw_text}
                </b>
                <span
                  style={{
                    fontSize: "13px",
                    fontWeight: 600,
                    color: "#5E5E5E",
                  }}
                >
                  {note.slide_title ??
                    (note.source === "chat"
                      ? "From tutor chat"
                      : "From lesson")}
                </span>
                <div
                  style={{
                    display: "flex",
                    gap: "6px",
                    flexWrap: "wrap",
                    marginTop: "4px",
                  }}
                >
                  {note.status === "waiting_to_sync" ? (
                    <span
                      style={{
                        padding: "3px 8px",
                        borderRadius: "12px",
                        fontWeight: 800,
                        fontSize: "11px",
                        background: "#FFF1D1",
                        color: "#704400",
                      }}
                    >
                      Waiting to sync
                    </span>
                  ) : note.status === "enrichment_pending" ? (
                    <span
                      style={{
                        padding: "3px 8px",
                        borderRadius: "12px",
                        fontWeight: 800,
                        fontSize: "11px",
                        background: "#FFF1D1",
                        color: "#704400",
                      }}
                    >
                      Enrichment pending
                    </span>
                  ) : note.status === "enriched" ? (
                    <span
                      style={{
                        padding: "3px 8px",
                        borderRadius: "12px",
                        fontWeight: 800,
                        fontSize: "11px",
                        background: "#F0E7FF",
                        color: "#5B2BB5",
                      }}
                    >
                      Enriched
                    </span>
                  ) : null}
                  <span
                    style={{
                      padding: "3px 8px",
                      borderRadius: "12px",
                      fontWeight: 800,
                      fontSize: "11px",
                      background: "#F1F1F1",
                      color: "#444",
                    }}
                  >
                    {note.reviewed_at ? "Reviewed" : "Not reviewed"}
                  </span>
                </div>
              </button>
            );
          })}
        </div>
      </div>

      {/* Detail View */}
      <div
        className="notes-detail"
        style={{
          flex: 1,
          padding: "28px 36px",
          display: "flex",
          flexDirection: "column",
          gap: "16px",
          minWidth: 0,
        }}
      >
        {selectedNote ? (
          <>
            <div
              style={{
                background: "#fff",
                border: "2px solid #E3E3E3",
                borderRadius: "18px",
                padding: "18px 20px",
                display: "flex",
                flexDirection: "column",
                gap: "10px",
              }}
            >
              <div style={{ display: "flex", alignItems: "center" }}>
                <b style={{ flex: 1, fontSize: "16px", color: "#2B2B2B" }}>
                  Your original note
                </b>
              </div>
              <p
                style={{
                  margin: 0,
                  fontSize: "15px",
                  lineHeight: 1.5,
                  color: "#2B2B2B",
                  whiteSpace: "pre-wrap",
                }}
              >
                {selectedNote.raw_text}
              </p>
              <span
                style={{ fontSize: "13px", fontWeight: 800, color: "#1667AB" }}
              >
                From:{" "}
                {selectedNote.slide_title ??
                  (selectedNote.source === "chat" ? "Tutor chat" : "Lesson")}
              </span>
            </div>

            <div
              style={{
                background: "#FBF8FF",
                border: "2px solid #DCC9FB",
                borderRadius: "18px",
                padding: "18px 20px",
                display: "flex",
                flexDirection: "column",
                gap: "10px",
              }}
            >
              <b style={{ color: "#5B2BB5", fontSize: "16px" }}>AI additions</b>
              {selectedNote.summary ? (
                <>
                  <p
                    style={{
                      margin: 0,
                      fontSize: "14px",
                      lineHeight: 1.5,
                      color: "#2B2B2B",
                    }}
                  >
                    <b>Summary:</b> {selectedNote.summary}
                  </p>
                  {selectedNote.tags && selectedNote.tags.length > 0 && (
                    <div
                      style={{ display: "flex", gap: "6px", flexWrap: "wrap" }}
                    >
                      {selectedNote.tags.map((tag) => (
                        <span
                          key={tag}
                          style={{
                            padding: "3px 8px",
                            borderRadius: "12px",
                            fontSize: "12px",
                            fontWeight: 800,
                            background: "#F1F1F1",
                            color: "#444",
                          }}
                        >
                          #{tag}
                        </span>
                      ))}
                    </div>
                  )}
                </>
              ) : (
                <p style={{ margin: 0, fontSize: "14px", color: "#704400" }}>
                  Enrichment pending. The AI summary and tags appear here once
                  enriched.
                </p>
              )}
            </div>
          </>
        ) : (
          <div style={{ color: "#5E5E5E", fontSize: "15px" }}>
            Select a note to view its details.
          </div>
        )}
      </div>
    </div>
  );
}
