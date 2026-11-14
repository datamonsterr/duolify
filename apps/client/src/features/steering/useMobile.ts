import { useSyncExternalStore } from "react";
const query = "(max-width: 47.999rem)";
function subscribe(notify: () => void) {
  if (!window.matchMedia) return () => {};
  const m = window.matchMedia(query);
  m.addEventListener("change", notify);
  return () => m.removeEventListener("change", notify);
}
export function useMobile() {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia?.(query).matches ?? false,
    () => false,
  );
}
