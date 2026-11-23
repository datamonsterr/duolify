import { useEffect, useState } from "react";
import { characterLibrary, type CharacterRecord } from "./library";

// One shared, short-lived read of the Learner's Characters for the guide slot and the picker.
let cache: { at: number; value: Promise<CharacterRecord[]> } | undefined;
export const resetMascotCharacters = () => {
  cache = undefined;
};
function load(): Promise<CharacterRecord[]> {
  if (!cache || Date.now() - cache.at > 60_000) {
    const value = characterLibrary.list();
    cache = { at: Date.now(), value };
    value.catch(() => {
      if (cache?.value === value) cache = undefined;
    });
  }
  return cache.value;
}
/** `undefined` while loading, `null` if unavailable (offline, feature off), else the list. */
export function useOwnCharacters(): CharacterRecord[] | null | undefined {
  const [list, set] = useState<CharacterRecord[] | null | undefined>();
  useEffect(() => {
    let live = true;
    load().then(
      (v) => live && set(v),
      () => live && set(null),
    );
    return () => {
      live = false;
    };
  }, []);
  return list;
}
