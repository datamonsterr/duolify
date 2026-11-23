import { useCallback, useEffect, useRef, useState } from "react";
import { session } from "../accounts/session";
import { createDraftStore } from "./drafts";
import type { DraftRequest, DraftStore } from "./drafts";
import type { Draft } from "./engine";

const request: DraftRequest = (path, method, body, headers) =>
  session.request(path, method, body, headers);

/** Loads the saved draft for a signed-in Learner, then keeps it in sync after each answer.
 * `ready` is false until the first load settles, so the chat resumes at the right step. */
export function useGuidedDraft(scriptId: string, owner: string) {
  const store = useRef<DraftStore | null>(null);
  const [state, setState] = useState<{ ready: boolean; initial: Draft | null }>(
    {
      ready: false,
      initial: null,
    },
  );
  useEffect(() => {
    let live = true;
    const next = createDraftStore({ scriptId, owner, request });
    store.current = next;
    next
      .load()
      .catch(() => null)
      .then((initial) => live && setState({ ready: true, initial }));
    return () => {
      live = false;
      void next.flush();
      next.dispose();
    };
  }, [scriptId, owner]);
  const onDraft = useCallback(
    (draft: Draft | null) => store.current?.save(draft),
    [],
  );
  const clear = useCallback(
    () => store.current?.clear() ?? Promise.resolve(),
    [],
  );
  return { ...state, onDraft, clear };
}
