import { ActionFooter } from "../design/sheet/ActionFooter";
import { useId, useState } from "react";

/** The same informed consent step is used by initial publication and republishing. */
export function CharacterExportConfirmation({
  names,
  online,
  busy,
  onConfirm,
}: {
  names: string[];
  online: boolean;
  busy: boolean;
  onConfirm: () => void;
}) {
  const id = useId();
  const [confirmed, setConfirmed] = useState(false);
  return (
    <section className="sharing-draft" aria-labelledby={`${id}-title`}>
      <h3 id={`${id}-title`}>Characters travel with this Course</h3>
      <p>
        Their name, bio, backstory, speaking style, persona and appearance
        become Course content. Enrolled copies keep these versions after you
        edit or delete your private Characters.
      </p>
      <ul aria-label="Attached Characters">
        {names.map((name, index) => (
          <li key={index}>
            <bdi>{name}</bdi>
          </li>
        ))}
      </ul>
      <p>
        Your creation prompts, private library links and screening records stay
        private.
      </p>
      <label className="sharing-confirm">
        <input
          type="checkbox"
          checked={confirmed}
          disabled={busy}
          onChange={(e) => setConfirmed(e.target.checked)}
        />
        I confirm these Characters can become Course content
      </label>
      <ActionFooter>
        <button
          type="button"
          className="duo-button"
          disabled={!confirmed || !online || busy}
          onClick={onConfirm}
        >
          Confirm Character export
        </button>
      </ActionFooter>
    </section>
  );
}
