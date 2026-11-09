import { useState } from "react";
import { Button } from "../design/Button";
import { reservationAPI, type Reservation } from "./reservations";

export function ReservationPanel({ quoteId }: { quoteId: string }) {
  const [job, setJob] = useState<Reservation>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  async function act(release = false) {
    setBusy(true);
    setError(undefined);
    try {
      setJob(
        await (release && job
          ? reservationAPI.release(job.id)
          : reservationAPI.reserve(quoteId)),
      );
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Check your connection and retry. Your work is saved.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="reservation-panel">
      {error && <p role="alert">{error}</p>}
      {busy && <p role="status">Confirming reservation…</p>}
      {job ? (
        <>
          <p role="status" data-testid="reservation" data-job-id={job.id}>
            {job.state === "released"
              ? "Credits released. Your work is saved."
              : "Credits reserved · original version retained."}
          </p>
          {job.state === "reserved" && (
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => void act(true)}
            >
              Release reservation
            </Button>
          )}
        </>
      ) : (
        <Button disabled={busy} onClick={() => void act()}>
          Reserve credits
        </Button>
      )}
    </div>
  );
}
