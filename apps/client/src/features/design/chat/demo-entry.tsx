import { UiPreferencesProvider } from "../localization/Preferences";
import { createRoot } from "react-dom/client";
import "../../../styles.css";
import { GuidedChatDemo } from "./GuidedChatDemo";

createRoot(document.getElementById("root")!).render(
  <UiPreferencesProvider>
    <GuidedChatDemo />
  </UiPreferencesProvider>,
);
