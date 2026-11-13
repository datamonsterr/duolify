import { useEffect, useState } from "react";
import { session } from "../accounts/session";
import { useMobileLayout } from "../shell/AdaptiveLayouts";
import { noteQueue, type QueuedNote } from "./note-queue";
import {
  trackExportNotes,
  trackQuickNoteDeleted,
  trackQuickNoteReviewed,
} from "../../lib/analytics";

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
    | "enrichment_pending"
    | "enriching"
    | "enriched"
    | "enrichment_failed"
    | "raw_only"
    | "waiting_to_sync";
  summary?: string | null;
  tags?: string[] | null;
  links?: string[] | null;
  /** Set for notes saved from tutor chat; `unavailable` once that thread was deleted. */
  chat_source?: { state: "available" | "unavailable" } | null;
  reviewed?: boolean;
  reviewed_at?: string | null;
  version?: number;
  created_at: string;
  updated_at: string;
};

function generateNoteMarkdown(note: QuickNoteItem): string {
  const title =
    note.slide_title && note.slide_title.trim()
      ? note.slide_title
      : "Quick Note";
  let md = `# ${title}\n\n## Your original note\n\n${note.raw_text}\n`;
  const hasEnrichment =
    Boolean(note.summary && note.summary.trim()) ||
    Boolean(note.tags && note.tags.length > 0) ||
    Boolean(note.links && note.links.length > 0);
  if (hasEnrichment) {
    md += `\n## AI additions\n\n`;
    if (note.summary && note.summary.trim()) {
      md += `Summary: ${note.summary}\n\n`;
    }
    if (note.tags && note.tags.length > 0) {
      md += `### Tags\n${note.tags.map((t) => (t.startsWith("#") ? t : `#${t}`)).join(" ")}\n\n`;
    }
    if (note.links && note.links.length > 0) {
      md += `### Related links\n${note.links.map((l) => `- ${l}`).join("\n")}\n\n`;
    }
  }
  return md;
}

function downloadMarkdownFile(filename: string, content: string) {
  const blob = new Blob([content], { type: "text/markdown;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

export function NotesPage() {
  const isMobile = useMobileLayout();
  const [notes, setNotes] = useState<QuickNoteItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchQuery, setSearchQuery] = useState("");
  const [filterNotReviewed, setFilterNotReviewed] = useState(false);
  const [selectedTag, setSelectedTag] = useState<string | null>(null);
  const [selectedCourse, setSelectedCourse] = useState<string | null>(null);
  const [selectedNoteId, setSelectedNoteId] = useState<string | null>(null);
  const [retryingId, setRetryingId] = useState<string | null>(null);

  // Edit and revision concurrency states
  const [isEditing, setIsEditing] = useState(false);
  const [editText, setEditText] = useState("");
  const [editError, setEditError] = useState<string | null>(null);
  const [savingEdit, setSavingEdit] = useState(false);
  const [conflictState, setConflictState] = useState<{
    localText: string;
    remoteNote: QuickNoteItem;
  } | null>(null);
  const [conflictChoice, setConflictChoice] = useState<"local" | "remote">(
    "local",
  );

  const handleRetryEnrichment = async (noteId: string) => {
    setRetryingId(noteId);
    try {
      const updated = await session.request<QuickNoteItem>(
        `/v1/notes/${noteId}/enrich`,
        "POST",
      );
      if (updated) {
        setNotes((prev) =>
          prev.map((n) => (n.id === noteId ? { ...n, ...updated } : n)),
        );
      }
    } catch {
      // ignore retry failure
    } finally {
      setRetryingId(null);
    }
  };

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
          version: 1,
          reviewed: false,
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
        if (combined.length > 0 && !isMobile) {
          setSelectedNoteId((curr) => curr ?? combined[0].id);
        }
      } catch {
        if (cancelled) return;
        // On offline or error, display local queued notes
        setNotes(queuedItems);
        if (queuedItems.length > 0 && !isMobile) {
          setSelectedNoteId((curr) => curr ?? queuedItems[0].id);
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
  }, [isMobile]);

  useEffect(() => {
    const hasPending = notes.some(
      (n) => n.status === "enrichment_pending" || n.status === "enriching",
    );
    if (!hasPending) return;

    const timer = setInterval(() => {
      void (async () => {
        try {
          const fetched = await session.request<
            { notes: QuickNoteItem[] } | QuickNoteItem[]
          >("/v1/notes");
          const serverNotes = Array.isArray(fetched)
            ? fetched
            : (fetched?.notes ?? []);
          setNotes((prev) => {
            const serverMap = new Map(serverNotes.map((s) => [s.id, s]));
            return prev.map((n) => {
              const updated = serverMap.get(n.id);
              if (!updated) return n;
              if ((n.version ?? 1) > (updated.version ?? 1)) {
                return { ...updated, raw_text: n.raw_text, version: n.version };
              }
              return { ...n, ...updated };
            });
          });
        } catch {
          // ignore
        }
      })();
    }, 1000);

    return () => clearInterval(timer);
  }, [notes]);

  const filteredNotes = notes.filter((n) => {
    if (filterNotReviewed && (n.reviewed || n.reviewed_at)) return false;
    if (selectedCourse && n.course_id !== selectedCourse) return false;
    if (selectedTag && !n.tags?.includes(selectedTag)) return false;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      const matchText = n.raw_text.toLowerCase().includes(q);
      const matchTitle = n.slide_title?.toLowerCase().includes(q) ?? false;
      const matchSummary = n.summary?.toLowerCase().includes(q) ?? false;
      const matchTags =
        n.tags?.some((t) => t.toLowerCase().includes(q)) ?? false;
      if (!matchText && !matchTitle && !matchSummary && !matchTags)
        return false;
    }
    return true;
  });

  const selectedNote = isMobile
    ? (filteredNotes.find((n) => n.id === selectedNoteId) ?? null)
    : (filteredNotes.find((n) => n.id === selectedNoteId) ??
      filteredNotes[0] ??
      null);

  const clearAllFilters = () => {
    setSearchQuery("");
    setFilterNotReviewed(false);
    setSelectedTag(null);
    setSelectedCourse(null);
  };

  const hasActiveFilters = Boolean(
    searchQuery.trim() || filterNotReviewed || selectedTag || selectedCourse,
  );

  // Edit handling
  const handleStartEdit = () => {
    if (!selectedNote) return;
    setIsEditing(true);
    setEditText(selectedNote.raw_text);
    setEditError(null);
    setConflictState(null);
  };

  const handleCancelEdit = () => {
    setIsEditing(false);
    setEditError(null);
    setConflictState(null);
  };

  const handleSaveEdit = async () => {
    if (!selectedNote) return;
    if (!editText.trim()) {
      setEditError("Note text cannot be empty.");
      return;
    }
    if (editText.length > 2000) {
      setEditError("Note text exceeds 2000 characters.");
      return;
    }

    setSavingEdit(true);
    setEditError(null);
    try {
      const updated = await session.request<QuickNoteItem>(
        `/v1/notes/${selectedNote.id}`,
        "PATCH",
        {
          raw_text: editText,
          expected_version: selectedNote.version ?? 1,
        },
      );
      if (updated) {
        setNotes((prev) =>
          prev.map((n) =>
            n.id === selectedNote.id ? { ...n, ...updated } : n,
          ),
        );
      } else {
        setNotes((prev) =>
          prev.map((n) =>
            n.id === selectedNote.id
              ? {
                  ...n,
                  raw_text: editText,
                  version: (n.version ?? 1) + 1,
                  updated_at: new Date().toISOString(),
                }
              : n,
          ),
        );
      }
      setIsEditing(false);
      setConflictState(null);
    } catch (err: unknown) {
      const errorObj = err as {
        status?: number;
        message?: string;
        serverNote?: QuickNoteItem;
      };
      if (
        errorObj?.status === 409 ||
        errorObj?.message?.includes("Conflict") ||
        errorObj?.message?.includes("conflict") ||
        errorObj?.serverNote
      ) {
        const remoteNote = errorObj?.serverNote ?? {
          ...selectedNote,
          raw_text: errorObj?.serverNote?.raw_text ?? selectedNote.raw_text,
          version: (selectedNote.version ?? 1) + 1,
        };
        setConflictState({
          localText: editText,
          remoteNote,
        });
        setConflictChoice("local");
      } else {
        setEditError(errorObj?.message ?? "Failed to save note");
      }
    } finally {
      setSavingEdit(false);
    }
  };

  const handleResolveConflict = async () => {
    if (!conflictState || !selectedNote) return;
    if (conflictChoice === "remote") {
      setNotes((prev) =>
        prev.map((n) =>
          n.id === selectedNote.id ? { ...n, ...conflictState.remoteNote } : n,
        ),
      );
      setConflictState(null);
      setIsEditing(false);
      return;
    }

    // Keep local version: submit edit with latest version
    setSavingEdit(true);
    try {
      const updated = await session.request<QuickNoteItem>(
        `/v1/notes/${selectedNote.id}`,
        "PATCH",
        {
          raw_text: conflictState.localText,
          expected_version:
            conflictState.remoteNote.version ?? (selectedNote.version ?? 1) + 1,
        },
      );
      if (updated) {
        setNotes((prev) =>
          prev.map((n) =>
            n.id === selectedNote.id ? { ...n, ...updated } : n,
          ),
        );
      } else {
        setNotes((prev) =>
          prev.map((n) =>
            n.id === selectedNote.id
              ? {
                  ...n,
                  raw_text: conflictState.localText,
                  version: (conflictState.remoteNote.version ?? 1) + 1,
                  updated_at: new Date().toISOString(),
                }
              : n,
          ),
        );
      }
      setConflictState(null);
      setIsEditing(false);
    } catch (err: unknown) {
      const errorObj = err as { message?: string };
      setEditError(errorObj?.message ?? "Failed to resolve conflict");
    } finally {
      setSavingEdit(false);
    }
  };

  // Mark reviewed handling
  const handleMarkReviewed = async (noteId: string) => {
    try {
      const updated = await session.request<QuickNoteItem>(
        `/v1/notes/${noteId}/review`,
        "POST",
      );
      trackQuickNoteReviewed(noteId);
      if (updated) {
        setNotes((prev) =>
          prev.map((n) =>
            n.id === noteId
              ? {
                  ...n,
                  ...updated,
                  reviewed: true,
                  reviewed_at: updated.reviewed_at ?? new Date().toISOString(),
                }
              : n,
          ),
        );
      } else {
        setNotes((prev) =>
          prev.map((n) =>
            n.id === noteId
              ? { ...n, reviewed: true, reviewed_at: new Date().toISOString() }
              : n,
          ),
        );
      }
    } catch {
      trackQuickNoteReviewed(noteId);
      setNotes((prev) =>
        prev.map((n) =>
          n.id === noteId
            ? { ...n, reviewed: true, reviewed_at: new Date().toISOString() }
            : n,
        ),
      );
    }
  };

  // Markdown export handling
  const handleExportMarkdown = async (note: QuickNoteItem) => {
    trackExportNotes();
    try {
      const md = await session.request<string>(
        `/v1/notes/${note.id}/export`,
        "GET",
      );
      if (typeof md === "string" && md.trim()) {
        downloadMarkdownFile(`note-${note.id}.md`, md);
        return;
      }
    } catch {
      // Fallback to client-side format
    }
    downloadMarkdownFile(`note-${note.id}.md`, generateNoteMarkdown(note));
  };

  // Delete note handling
  const handleDeleteNote = async (noteId: string) => {
    try {
      await session.request(`/v1/notes/${noteId}`, "DELETE");
    } catch {
      // Proceed to update state
    }
    trackQuickNoteDeleted(noteId);
    setNotes((prev) => prev.filter((n) => n.id !== noteId));
    if (selectedNoteId === noteId) {
      setSelectedNoteId(null);
    }
    setIsEditing(false);
    setConflictState(null);
  };

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
      {(!isMobile || !selectedNoteId) && (
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
                borderColor: !hasActiveFilters ? "#8CCBF7" : "#E0E0E0",
                background: !hasActiveFilters ? "#DDF1FF" : "#fff",
                color: !hasActiveFilters ? "#0D5592" : "#2B2B2B",
              }}
              aria-pressed={!hasActiveFilters}
              onClick={clearAllFilters}
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
            {filteredNotes.length}{" "}
            {filteredNotes.length === 1 ? "note" : "notes"}
            {hasActiveFilters && (
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
                  onClick={clearAllFilters}
                >
                  Clear filters
                </button>
              </>
            )}
          </span>

          {/* Empty State when no notes at all */}
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
              <a
                href="/courses"
                className="duo-button primary"
                style={{
                  minHeight: "44px",
                  padding: "0 20px",
                  fontSize: "14px",
                  display: "inline-flex",
                  alignItems: "center",
                  justifyContent: "center",
                  textDecoration: "none",
                }}
              >
                Go to lesson
              </a>
            </div>
          )}

          {/* No results when filters active */}
          {!loading && notes.length > 0 && filteredNotes.length === 0 && (
            <div
              style={{
                padding: "24px 16px",
                textAlign: "center",
                display: "flex",
                flexDirection: "column",
                gap: "8px",
              }}
            >
              <b style={{ color: "#2B2B2B", fontSize: "15px" }}>
                No notes match
              </b>
              <span style={{ fontSize: "13px", color: "#5E5E5E" }}>
                0 notes match{" "}
                {searchQuery ? `“${searchQuery}”` : "the selected filters"} ·{" "}
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
                  onClick={clearAllFilters}
                >
                  Clear filters to see more notes
                </button>
              </span>
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
              const isReviewed = Boolean(note.reviewed || note.reviewed_at);
              return (
                <button
                  key={note.id}
                  type="button"
                  aria-label={`${note.slide_title && note.slide_title.trim() ? `${note.slide_title}: ` : ""}${note.raw_text}`}
                  onClick={() => {
                    setSelectedNoteId(note.id);
                    setIsEditing(false);
                    setConflictState(null);
                  }}
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
                    {note.slide_title && note.slide_title.trim()
                      ? note.slide_title
                      : note.source === "chat"
                        ? note.chat_source?.state === "unavailable"
                          ? "From tutor chat · source unavailable"
                          : "From tutor chat"
                        : "From lesson"}
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
                    ) : note.status === "enriching" ? (
                      <span
                        style={{
                          padding: "3px 8px",
                          borderRadius: "12px",
                          fontWeight: 800,
                          fontSize: "11px",
                          background: "#E0F2FE",
                          color: "#0369A1",
                        }}
                      >
                        Enriching...
                      </span>
                    ) : note.status === "enrichment_failed" ? (
                      <span
                        style={{
                          padding: "3px 8px",
                          borderRadius: "12px",
                          fontWeight: 800,
                          fontSize: "11px",
                          background: "#FEE2E2",
                          color: "#B91C1C",
                        }}
                      >
                        Enrichment failed
                      </span>
                    ) : note.status === "raw_only" ? (
                      <span
                        style={{
                          padding: "3px 8px",
                          borderRadius: "12px",
                          fontWeight: 800,
                          fontSize: "11px",
                          background: "#F1F1F1",
                          color: "#555",
                        }}
                      >
                        Raw only
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
                        background: isReviewed ? "#E3F8D3" : "#F1F1F1",
                        color: isReviewed ? "#1E6E0C" : "#444",
                      }}
                    >
                      {isReviewed ? "Reviewed" : "Not reviewed"}
                    </span>
                  </div>
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Detail View */}
      {(!isMobile || selectedNoteId) && (
        <div
          className="notes-detail"
          style={{
            flex: 1,
            padding: isMobile ? "16px" : "28px 36px",
            display: "flex",
            flexDirection: "column",
            gap: "16px",
            minWidth: 0,
          }}
        >
          {isMobile && selectedNoteId && (
            <button
              type="button"
              className="duo-button quiet"
              onClick={() => {
                setSelectedNoteId(null);
                setIsEditing(false);
                setConflictState(null);
              }}
              style={{
                alignSelf: "flex-start",
                padding: "6px 12px",
                borderRadius: "12px",
                fontWeight: 800,
                fontSize: "13px",
                border: "2px solid #E0E0E0",
                background: "#fff",
                color: "#2B2B2B",
                cursor: "pointer",
                marginBottom: "4px",
              }}
            >
              ← Back to notes
            </button>
          )}

          {selectedNote ? (
            <>
              {/* Conflict banner */}
              {conflictState && (
                <div
                  style={{
                    background: "#FFF1D1",
                    border: "2px solid #F6D884",
                    borderRadius: "18px",
                    padding: "16px 18px",
                    display: "flex",
                    flexDirection: "column",
                    gap: "12px",
                  }}
                  role="alert"
                >
                  <div>
                    <b
                      style={{
                        fontSize: "15px",
                        fontWeight: 900,
                        color: "#704400",
                        display: "block",
                        marginBottom: "4px",
                      }}
                    >
                      This note changed on another device
                    </b>
                    <p
                      style={{ margin: 0, fontSize: "13px", color: "#704400" }}
                    >
                      Pick which version to keep. Nothing is lost until you
                      choose.
                    </p>
                  </div>

                  <div
                    style={{
                      display: "flex",
                      flexDirection: "column",
                      gap: "8px",
                    }}
                  >
                    <label
                      style={{
                        display: "flex",
                        alignItems: "flex-start",
                        gap: "10px",
                        background: "#fff",
                        border: "2px solid",
                        borderColor:
                          conflictChoice === "local" ? "#8CCBF7" : "#E3E3E3",
                        borderRadius: "14px",
                        padding: "10px 12px",
                        cursor: "pointer",
                      }}
                    >
                      <input
                        type="radio"
                        name="conflict_resolution"
                        checked={conflictChoice === "local"}
                        onChange={() => setConflictChoice("local")}
                        style={{ marginTop: "3px" }}
                      />
                      <div>
                        <b style={{ fontSize: "14px", color: "#2B2B2B" }}>
                          This device
                        </b>
                        <p
                          style={{
                            margin: "4px 0 0",
                            fontSize: "13px",
                            color: "#555",
                            whiteSpace: "pre-wrap",
                          }}
                        >
                          {conflictState.localText}
                        </p>
                      </div>
                    </label>

                    <label
                      style={{
                        display: "flex",
                        alignItems: "flex-start",
                        gap: "10px",
                        background: "#fff",
                        border: "2px solid",
                        borderColor:
                          conflictChoice === "remote" ? "#8CCBF7" : "#E3E3E3",
                        borderRadius: "14px",
                        padding: "10px 12px",
                        cursor: "pointer",
                      }}
                    >
                      <input
                        type="radio"
                        name="conflict_resolution"
                        checked={conflictChoice === "remote"}
                        onChange={() => setConflictChoice("remote")}
                        style={{ marginTop: "3px" }}
                      />
                      <div>
                        <b style={{ fontSize: "14px", color: "#2B2B2B" }}>
                          Other device
                        </b>
                        <p
                          style={{
                            margin: "4px 0 0",
                            fontSize: "13px",
                            color: "#555",
                            whiteSpace: "pre-wrap",
                          }}
                        >
                          {conflictState.remoteNote.raw_text}
                        </p>
                      </div>
                    </label>
                  </div>

                  <button
                    type="button"
                    className="duo-button primary"
                    onClick={handleResolveConflict}
                    disabled={savingEdit}
                    style={{
                      alignSelf: "flex-start",
                      padding: "8px 18px",
                      borderRadius: "12px",
                      fontWeight: 800,
                      fontSize: "13px",
                      background: "#58CC02",
                      color: "#fff",
                      border: "none",
                      cursor: "pointer",
                    }}
                  >
                    Keep selected
                  </button>
                </div>
              )}

              {/* Original note card */}
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
                  {!isEditing && (
                    <button
                      type="button"
                      className="duo-button quiet"
                      onClick={handleStartEdit}
                      aria-label="Edit"
                      style={{
                        background: "none",
                        border: "none",
                        color: "#1667AB",
                        fontWeight: 900,
                        fontSize: "13px",
                        cursor: "pointer",
                        padding: "4px 8px",
                      }}
                    >
                      Edit
                    </button>
                  )}
                </div>

                {isEditing ? (
                  <div
                    style={{
                      display: "flex",
                      flexDirection: "column",
                      gap: "10px",
                    }}
                  >
                    <textarea
                      aria-label="Edit note text"
                      value={editText}
                      onChange={(e) => setEditText(e.target.value)}
                      maxLength={2000}
                      rows={5}
                      style={{
                        width: "100%",
                        padding: "10px 12px",
                        borderRadius: "12px",
                        border: "2px solid #CFCFCF",
                        fontFamily: "inherit",
                        fontSize: "15px",
                        lineHeight: 1.5,
                        resize: "vertical",
                        boxSizing: "border-box",
                      }}
                    />
                    <div
                      style={{
                        display: "flex",
                        justifyContent: "space-between",
                        alignItems: "center",
                      }}
                    >
                      <span style={{ fontSize: "12px", color: "#5E5E5E" }}>
                        {editText.length} / 2,000
                      </span>
                      {editError && (
                        <span
                          style={{
                            fontSize: "13px",
                            color: "#B91C1C",
                            fontWeight: 700,
                          }}
                        >
                          {editError}
                        </span>
                      )}
                    </div>
                    <div style={{ display: "flex", gap: "8px" }}>
                      <button
                        type="button"
                        className="duo-button primary"
                        disabled={savingEdit}
                        onClick={handleSaveEdit}
                        style={{
                          padding: "8px 16px",
                          borderRadius: "12px",
                          fontWeight: 800,
                          fontSize: "13px",
                          background: "#58CC02",
                          color: "#fff",
                          border: "none",
                          cursor: "pointer",
                        }}
                      >
                        {savingEdit ? "Saving..." : "Save changes"}
                      </button>
                      <button
                        type="button"
                        className="duo-button quiet"
                        disabled={savingEdit}
                        onClick={handleCancelEdit}
                        style={{
                          padding: "8px 16px",
                          borderRadius: "12px",
                          fontWeight: 800,
                          fontSize: "13px",
                          background: "#fff",
                          color: "#2B2B2B",
                          border: "2px solid #E0E0E0",
                          cursor: "pointer",
                        }}
                      >
                        Cancel
                      </button>
                    </div>
                  </div>
                ) : (
                  <>
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
                      style={{
                        fontSize: "13px",
                        fontWeight: 800,
                        color: "#1667AB",
                      }}
                    >
                      From:{" "}
                      {selectedNote.slide_title &&
                      selectedNote.slide_title.trim()
                        ? selectedNote.slide_title
                        : selectedNote.source === "chat"
                          ? selectedNote.chat_source?.state === "unavailable"
                            ? "Tutor chat · source unavailable"
                            : "Tutor chat"
                          : "Lesson"}
                    </span>
                  </>
                )}
              </div>

              {/* AI additions card */}
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
                <b style={{ color: "#5B2BB5", fontSize: "16px" }}>
                  AI additions
                </b>
                {selectedNote.status === "enrichment_failed" ? (
                  <div
                    style={{
                      background: "#FFF4F2",
                      border: "2px solid #FCDAD7",
                      borderRadius: "14px",
                      padding: "14px 16px",
                      display: "flex",
                      flexDirection: "column",
                      gap: "10px",
                    }}
                  >
                    <p
                      style={{
                        margin: 0,
                        fontWeight: 700,
                        fontSize: "14px",
                        color: "#D32F2F",
                        lineHeight: 1.4,
                      }}
                    >
                      Couldn't add the AI summary. Your note is safe. No credits
                      were charged.
                    </p>
                    <button
                      type="button"
                      className="duo-button primary"
                      disabled={retryingId === selectedNote.id}
                      onClick={() =>
                        void handleRetryEnrichment(selectedNote.id)
                      }
                      style={{
                        alignSelf: "flex-start",
                        padding: "8px 16px",
                        fontSize: "13px",
                        fontWeight: 800,
                        borderRadius: "12px",
                        border: "none",
                        background: "#58CC02",
                        color: "#fff",
                        cursor: "pointer",
                      }}
                    >
                      {
                        retryingId === selectedNote.id
                          ? "Retrying..."
                          : "Retry enrichment · 2 credits" /* @noncommercial-number */
                      }
                    </button>
                  </div>
                ) : selectedNote.summary ? (
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
                        style={{
                          display: "flex",
                          gap: "6px",
                          flexWrap: "wrap",
                        }}
                      >
                        {selectedNote.tags.map((tag) => (
                          <button
                            key={tag}
                            type="button"
                            onClick={() => setSelectedTag(tag)}
                            style={{
                              padding: "3px 8px",
                              borderRadius: "12px",
                              fontSize: "12px",
                              fontWeight: 800,
                              background: "#F1F1F1",
                              color: "#444",
                              border: "none",
                              cursor: "pointer",
                            }}
                          >
                            #{tag}
                          </button>
                        ))}
                      </div>
                    )}
                    {selectedNote.links && selectedNote.links.length > 0 && (
                      <div
                        style={{
                          display: "flex",
                          flexDirection: "column",
                          gap: "6px",
                          marginTop: "6px",
                        }}
                      >
                        <span
                          style={{
                            fontSize: "13px",
                            fontWeight: 800,
                            color: "#5B2BB5",
                          }}
                        >
                          Related:
                        </span>
                        <div
                          style={{
                            display: "flex",
                            gap: "8px",
                            flexWrap: "wrap",
                          }}
                        >
                          {selectedNote.links.map((link) => (
                            <a
                              key={link}
                              href="/courses"
                              style={{
                                padding: "4px 10px",
                                borderRadius: "10px",
                                fontSize: "12px",
                                fontWeight: 700,
                                background: "#EBF5FF",
                                color: "#1667AB",
                                textDecoration: "none",
                              }}
                            >
                              Lesson {link}
                            </a>
                          ))}
                        </div>
                      </div>
                    )}
                  </>
                ) : selectedNote.status === "enriching" ? (
                  <p style={{ margin: 0, fontSize: "14px", color: "#0369A1" }}>
                    Enriching note with AI summary and tags...
                  </p>
                ) : selectedNote.status === "raw_only" ? (
                  <p style={{ margin: 0, fontSize: "14px", color: "#555" }}>
                    Raw note saved without AI enrichment.
                  </p>
                ) : (
                  <p style={{ margin: 0, fontSize: "14px", color: "#704400" }}>
                    Enrichment pending. The AI summary and tags appear here once
                    enriched.
                  </p>
                )}
              </div>

              {/* Action buttons */}
              <div
                style={{
                  display: "flex",
                  gap: "10px",
                  flexWrap: "wrap",
                  alignItems: "center",
                }}
              >
                <button
                  type="button"
                  className="duo-button primary"
                  onClick={() => void handleMarkReviewed(selectedNote.id)}
                  style={{
                    padding: "10px 18px",
                    borderRadius: "14px",
                    fontWeight: 900,
                    fontSize: "14px",
                    background:
                      selectedNote.reviewed || selectedNote.reviewed_at
                        ? "#E3F8D3"
                        : "#1F7A0D",
                    color:
                      selectedNote.reviewed || selectedNote.reviewed_at
                        ? "#1E6E0C"
                        : "#fff",
                    border: "none",
                    cursor: "pointer",
                  }}
                >
                  {selectedNote.reviewed || selectedNote.reviewed_at
                    ? "Reviewed"
                    : "Mark reviewed"}
                </button>

                <button
                  type="button"
                  className="duo-button quiet"
                  onClick={() => void handleExportMarkdown(selectedNote)}
                  style={{
                    padding: "10px 18px",
                    borderRadius: "14px",
                    fontWeight: 800,
                    fontSize: "14px",
                    background: "#fff",
                    color: "#1667AB",
                    border: "2px solid #D3D3D3",
                    cursor: "pointer",
                  }}
                >
                  Export Markdown
                </button>

                <button
                  type="button"
                  className="duo-button quiet"
                  onClick={() => void handleDeleteNote(selectedNote.id)}
                  style={{
                    padding: "10px 18px",
                    borderRadius: "14px",
                    fontWeight: 800,
                    fontSize: "14px",
                    background: "#fff",
                    color: "#B3261E",
                    border: "2px solid #D3D3D3",
                    cursor: "pointer",
                  }}
                >
                  Delete
                </button>
              </div>
            </>
          ) : (
            <div style={{ color: "#5E5E5E", fontSize: "15px" }}>
              Select a note to view its details.
            </div>
          )}
        </div>
      )}
    </div>
  );
}
