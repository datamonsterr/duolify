import { UiPreferencesProvider } from "../localization/Preferences";
import { createRoot } from "react-dom/client";
import "../../../styles.css";
import { GuidedChatDemo } from "./GuidedChatDemo";
import { ScriptFixture } from "../../guided-chat/ScriptFixture";

const scripted =
  new URLSearchParams(window.location.search).get("fixture") === "script";
createRoot(document.getElementById("root")!).render(
  scripted ? (
    <ScriptFixture />
  ) : (
    <UiPreferencesProvider>
      <GuidedChatDemo />
    </UiPreferencesProvider>
  ),
);
