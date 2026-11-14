import { useRef, useState } from "react";
import { PublicationDraftPanel } from "./PublicationDraftPanel";

/** Owner entry point on the course view. */
export function PreviewPublicationDraft({ courseId }: { courseId: string }) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button
        ref={trigger}
        type="button"
        className="duo-button secondary"
        data-testid="preview-publication-draft"
        onClick={() => setOpen(true)}
      >
        Preview publication draft
      </button>
      {open && (
        <PublicationDraftPanel
          courseId={courseId}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  );
}
