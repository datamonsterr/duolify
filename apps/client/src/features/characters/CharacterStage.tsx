import { CharacterPreview } from "./CharacterPreview";
import { useCharacterT } from "./useCharacterT";
import type { RigView } from "./rig-v2";
import type { CharacterSpecV2, ExpressionV2, Pose } from "./types-v2";

/** Avatar and full-body previews side by side; both recompile on every draft change. */
export function CharacterStage({
  spec,
  pose = "idle",
  expression = "idle",
  size,
  tall,
}: {
  spec: CharacterSpecV2;
  pose?: Pose;
  expression?: ExpressionV2;
  size?: number;
  /** Desktop: fill the height of the host panel instead of fixed thumbnails. */
  tall?: boolean;
}) {
  const t = useCharacterT();
  const views: [RigView, string][] = [
    ["avatar", "characters.v2.view.avatar"],
    ["body", "characters.v2.view.body"],
  ];
  return (
    <div
      className={tall ? "cv2-stage-pair cv2-tall" : "cv2-stage-pair"}
      role="group"
      aria-label={t("characters.v2.preview")}
    >
      {views.map(([view, key]) => (
        <figure
          key={view}
          className={`cv2-fig cv2-fig-${view}`}
          data-testid={`preview-${view}`}
        >
          <CharacterPreview
            spec={spec}
            view={view}
            pose={pose}
            expression={expression}
            size={size}
          />
          <figcaption>{t(key)}</figcaption>
        </figure>
      ))}
    </div>
  );
}
