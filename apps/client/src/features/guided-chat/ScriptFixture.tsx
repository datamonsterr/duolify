import { useCallback, useState, useSyncExternalStore } from "react";
import { UiPreferencesProvider } from "../design/localization/Preferences";
import { GuidedScriptChat } from "./GuidedScriptChat";
import type { Draft } from "./engine";
import { allScripts } from "./scripts";
import { registerFixtureCopy } from "./messages.fixture";

registerFixtureCopy();

const subscribe = (notify: () => void) => {
  window.addEventListener("online", notify);
  window.addEventListener("offline", notify);
  return () => {
    window.removeEventListener("online", notify);
    window.removeEventListener("offline", notify);
  };
};

declare global {
  interface Window {
    __guided?: {
      submitted: unknown[];
      drafts: (Draft | null)[];
      events: unknown[];
    };
  }
}

/** Authored harness for e2e and screenshots. `?fixture=script&id=fixture.tour&locale=ar`. */
export function ScriptFixture() {
  const params = new URLSearchParams(window.location.search);
  const script =
    allScripts.find((s) => s.id === params.get("id")) ?? allScripts[0];
  const storageKey = `guided-fixture:${script.id}`;
  const [initial] = useState<Draft | null>(() => {
    try {
      return JSON.parse(sessionStorage.getItem(storageKey) ?? "null");
    } catch {
      return null;
    }
  });
  const online = useSyncExternalStore(
    subscribe,
    () => navigator.onLine,
    () => true,
  );
  window.__guided ??= { submitted: [], drafts: [], events: [] };
  const record = window.__guided;
  const onDraft = useCallback(
    (draft: Draft | null) => {
      record.drafts.push(draft);
      try {
        sessionStorage.setItem(storageKey, JSON.stringify(draft));
      } catch {
        /* storage may be unavailable */
      }
    },
    [record, storageKey],
  );
  return (
    <UiPreferencesProvider
      initialPreferences={{
        locale: params.get("locale") === "ar" ? "ar" : "en",
      }}
    >
      <main className="guided-demo">
        <GuidedScriptChat
          script={script}
          initialDraft={initial}
          online={online}
          onDraft={onDraft}
          onEvent={(event) => record.events.push(event)}
          onSubmit={(answers) => {
            record.submitted.push(answers);
            return Promise.resolve();
          }}
        />
      </main>
    </UiPreferencesProvider>
  );
}
