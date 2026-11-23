import { useEffect, type ReactNode } from "react";
import { trackMascot } from "../../lib/analytics";
import { useOwnCharacters } from "./ownCharacters";
import { compileRigSvg, validateCharacterSpec } from "./rig";
import type { Expression } from "./types";
import type { MascotState } from "../../components/mascot/rig-part-spec";

// Mascot states to Character expressions (custom-characters v1 has no `confused`; v2 maps it directly).
const EXPRESSION: Partial<Record<MascotState, Expression>> = {
  happy: "happy",
  celebrate: "celebrate",
  thinking: "thinking",
  confused: "sad",
};

/**
 * One of the Learner's own Characters as the guide. Persona, bio and backstory never reach this
 * slot. A missing, deleted or invalid Character shows `fallback` (built-in Tinko).
 */
export default function CharacterGuide({
  characterId,
  state,
  size,
  label,
  fallback,
}: {
  characterId: string;
  state: MascotState;
  size: number;
  label?: string;
  fallback: ReactNode;
}) {
  const data = useOwnCharacters();
  const record = data?.find((c) => c.id === characterId);
  const usable = !!record && validateCharacterSpec(record.spec).valid;
  const failed = data === null || (data !== undefined && !usable);
  useEffect(() => {
    if (failed)
      trackMascot({
        event: "mascot_state_fallback",
        reason: "character_unavailable",
      });
  }, [failed]);
  if (!usable || !record) return <>{fallback}</>;
  return (
    <span
      className="m-character"
      style={{
        display: "inline-block",
        width: size,
        height: size,
        pointerEvents: "none",
      }}
      {...(label
        ? { role: "img", "aria-label": label }
        : { "aria-hidden": true })}
      // Compiled from a validated closed-schema spec; no free text reaches the markup.
      dangerouslySetInnerHTML={{
        __html: compileRigSvg(record.spec, EXPRESSION[state] ?? "idle"),
      }}
    />
  );
}
