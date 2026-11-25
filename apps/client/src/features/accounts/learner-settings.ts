import { setFeedbackSettings } from "../design/feedback/feedbackBridge";
import type { LearnerSettings } from "./learner-context";
import { applyServerMascot } from "./mascot-settings";
import { authRequest, credentialStore, session } from "./session";

export async function accessToken() {
  await session.load();
  const tokens = await credentialStore.read();
  if (!tokens) throw new Error("Sign in again to edit your profile.");
  return tokens.access_token;
}

export async function readLearnerSettings() {
  const settings = await authRequest<LearnerSettings>(
    "/v1/me/settings",
    undefined,
    await accessToken(),
  );
  setFeedbackSettings({
    ui_sounds: settings.ui_sounds,
    haptics: settings.haptics,
  });
  applyServerMascot(settings.mascot);
  return settings;
}
