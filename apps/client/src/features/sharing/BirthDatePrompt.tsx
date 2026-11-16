import { useEffect, useId, useRef, useState } from "react";
import { session } from "../accounts/session";

/** Read only stable error codes; never display a response's private text. */
export function publisherAgeError(
  error: unknown,
): "birth_date_required" | "adult_required" | null {
  const failure = error as {
    status?: number;
    details?: { error?: string };
  } | null;
  if (
    failure?.status === 409 &&
    failure.details?.error === "birth_date_required"
  )
    return "birth_date_required";
  if (failure?.status === 403 && failure.details?.error === "adult_required")
    return "adult_required";
  return null;
}

/** Used inside the existing desktop Share dialog / mobile sheet. */
export function BirthDatePrompt({
  onSaved,
  onCancel,
}: {
  onSaved: () => void;
  onCancel: () => void;
}) {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const [date, setDate] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    const opener = document.activeElement;
    input.current?.focus();
    return () => {
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, []);

  async function save() {
    setError(null);
    if (!date) {
      setError(
        "Enter a real birth date from 1900 onward, no later than today.",
      );
      input.current?.focus();
      return;
    }
    setBusy(true);
    try {
      await session.request<void>("/v1/me/birth-date", "PUT", {
        birth_date: date,
      });
      setDate("");
      onSaved();
    } catch (failure) {
      setError(
        (failure as { status?: number } | null)?.status === 400
          ? "Enter a real birth date from 1900 onward, no later than today."
          : "Couldn't save your birth date. Check your connection and retry.",
      );
      input.current?.focus();
    } finally {
      setBusy(false);
    }
  }

  return (
    <form
      className="sharing-draft ph-no-capture"
      aria-label="Birth date for publishing"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <h3>Before you publish</h3>
      <p id={`${id}-help`}>
        You must be 18 or older to publish. Your birth date stays private and is
        used to check publishing eligibility.
      </p>
      <label htmlFor={`${id}-date`}>Birth date</label>
      <input
        ref={input}
        id={`${id}-date`}
        className="sharing-birth-date"
        type="date"
        autoComplete="bday"
        min="1900-01-01"
        value={date}
        required
        disabled={busy}
        aria-invalid={error ? true : undefined}
        aria-describedby={`${id}-help${error ? ` ${id}-error` : ""}`}
        onChange={(event) => {
          setDate(event.target.value);
          setError(null);
        }}
      />
      {error && (
        <p id={`${id}-error`} className="sharing-error" role="alert">
          {error}
        </p>
      )}
      {busy && <p role="status">Saving birth date…</p>}
      <div className="sharing-actions">
        <button className="duo-button" type="submit" disabled={busy}>
          Save and continue
        </button>
        <button
          className="duo-button secondary"
          type="button"
          disabled={busy}
          onClick={onCancel}
        >
          Cancel
        </button>
      </div>
    </form>
  );
}
