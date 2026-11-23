import { CharacterPreview } from "./CharacterPreview";
import type { CharacterSpecV2 } from "./types-v2";

export default function HeaderAvatar({
  document,
}: {
  document: CharacterSpecV2;
}) {
  return <CharacterPreview spec={document} size={32} label={false} />;
}
