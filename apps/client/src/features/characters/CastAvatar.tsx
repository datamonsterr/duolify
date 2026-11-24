import { CharacterPreview } from "./CharacterPreview";
import { compileRigSvg } from "./rig";
import type { CharacterSpec } from "./types";
import type { CharacterSpecV2 } from "./types-v2";

/** Small kind-aware avatar for lists: v2 document when present, else the v1 rig. */
export function CastAvatar({
  spec,
  document,
  size = 40,
}: {
  spec: CharacterSpec;
  document?: CharacterSpecV2;
  size?: number;
}) {
  if (document)
    return <CharacterPreview spec={document} size={size} label={false} />;
  return (
    <div
      className="cv2-preview"
      aria-hidden="true"
      style={{ width: size, height: size }}
      dangerouslySetInnerHTML={{ __html: compileRigSvg(spec, "idle") }}
    />
  );
}
