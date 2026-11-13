import { useEffect } from "react";
import { trackCharacterReactionShown } from "./analytics";
import { speakerFallback } from "./playback";
import { neutralReaction, parseReaction } from "./reactions";
import { compileRigSvg, getBundledFallbackSvg } from "./rig";
import type { LessonCastSnapshot } from "./types";

/**
 * Supportive reaction to an authoritative graded answer. Display only: it
 * reads the API reaction and the Lesson's pinned Cast, never grades, and falls
 * back to a static neutral expression on any invalid input.
 */
export function CharacterReaction({
  reaction: raw,
  cast,
  resultKey,
}: {
  reaction: unknown;
  cast?: LessonCastSnapshot;
  resultKey: string;
}) {
  const parsed = parseReaction(raw);
  const speakerKey = cast?.speakers.guide
    ? "guide"
    : Object.keys(cast?.speakers ?? {})[0];
  const speaker = speakerKey ? cast?.speakers[speakerKey] : undefined;
  const usable =
    !!cast && !!speaker && speakerFallback(cast, speakerKey) === null;
  // Broken assets keep the approved copy but show the static neutral expression.
  const reaction = parsed
    ? usable
      ? parsed
      : { ...parsed, expression: "idle" as const }
    : neutralReaction();
  const svg =
    usable && speaker
      ? compileRigSvg(speaker.spec, reaction.expression)
      : getBundledFallbackSvg("idle");

  useEffect(() => {
    if (!parsed || parsed.trigger === "neutral") return;
    trackCharacterReactionShown({
      reaction_trigger: parsed.trigger,
      speaker_key: speakerKey ?? "guide",
      expression: reaction.expression,
      fallback_used: !usable,
      result_key: resultKey,
    });
  }, [parsed?.trigger, reaction.expression, resultKey]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div
      data-testid="character-reaction"
      data-expression={reaction.expression}
      style={{
        display: "flex",
        alignItems: "center",
        gap: "12px",
        marginBottom: "8px",
      }}
    >
      <div
        aria-hidden="true"
        style={{ width: 56, height: 56, flex: "0 0 56px" }}
        // Generated only from a validated closed-schema spec or the bundled fallback.
        dangerouslySetInnerHTML={{ __html: svg }}
      />
      {reaction.copy && (
        <p style={{ margin: 0, fontWeight: 700 }}>{reaction.copy}</p>
      )}
    </div>
  );
}
