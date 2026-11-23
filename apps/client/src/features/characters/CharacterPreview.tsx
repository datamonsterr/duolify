import { useMemo } from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import { compileV2, POSES, type RigView } from "./rig-v2";
import { altText } from "./rig-v2-alt";
import {
  EXPRESSIONS_V2,
  type CharacterSpecV2,
  type ExpressionV2,
  type Pose,
} from "./types-v2";

/** Trusted rig output only: compileV2 emits no scripts, external references or filters. */
export function CharacterPreview({
  spec,
  view = "avatar",
  pose = "idle",
  expression = "idle",
  size,
  label = true,
}: {
  spec: CharacterSpecV2;
  view?: RigView;
  pose?: Pose;
  expression?: ExpressionV2;
  size?: number;
  label?: boolean;
}) {
  const {
    preferences: { locale },
  } = useUiPreferences();
  const svg = useMemo(() => {
    try {
      return compileV2(spec, view, pose, expression);
    } catch {
      return "";
    }
  }, [spec, view, pose, expression]);
  return (
    <div
      className="cv2-preview"
      role={label ? "img" : undefined}
      aria-label={
        label ? altText(spec, locale === "ar" ? "ar" : "en") : undefined
      }
      aria-hidden={label ? undefined : true}
      style={size ? { width: size, height: size } : undefined}
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}

export { POSES, EXPRESSIONS_V2 };
