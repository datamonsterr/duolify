import { createContext, useContext } from "react";
import type { CharacterRecord, CharacterSpecV2 } from "../characters/chatApi";

/** What the `character` widget reads from its host: the draft (create) or the Cast choices. */
export type CharacterDraftApi = {
  spec: CharacterSpecV2;
  /** A patch the shared schema refused on the last change; the widget shows an alert. */
  rejected: boolean;
  /** RFC 7386 merge patch, the same draft-patch contract the selectors use. */
  patch: (patch: Record<string, unknown>) => void;
  replace: (next: CharacterSpecV2) => void;
};
export type CastChoice = {
  id: string;
  name: string;
  record: Pick<CharacterRecord, "spec" | "document">;
};
/** `taken` maps a step id to the Character it picked, so a step can hide the others' picks. */
export type CastApi = { choices: CastChoice[]; taken: Record<string, string> };
export type CharacterChatApi = { draft?: CharacterDraftApi; cast?: CastApi };

export const CharacterChatContext = createContext<CharacterChatApi>({});
export const useCharacterChat = () => useContext(CharacterChatContext);
