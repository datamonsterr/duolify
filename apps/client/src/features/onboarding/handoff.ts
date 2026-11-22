// Ephemeral topic handoff, scoped to the current learner. No URL or analytics text.
const key = (owner: string) => `duolify.onboarding-topic:${owner}`;
export function handoffTopic(owner: string, topic: string) {
  sessionStorage.setItem(key(owner), topic.trim().slice(0, 2000));
  // A deliberate new topic starts a fresh brief rather than an abandoned draft.
  sessionStorage.removeItem(`course-brief:${owner}`);
}
export function takeTopic(owner: string): string | null {
  try {
    const value = sessionStorage.getItem(key(owner));
    sessionStorage.removeItem(key(owner));
    return value;
  } catch {
    return null;
  }
}
const replayKey = (owner: string) => `duolify.onboarding-replay:${owner}`;
export function requestReplay(owner: string) {
  try {
    sessionStorage.setItem(replayKey(owner), "true");
  } catch {
    /* In-app event covers denied storage. */
  }
  window.dispatchEvent(new CustomEvent("duolify:onboarding-replay"));
}
export function takeReplay(owner: string) {
  try {
    const value = sessionStorage.getItem(replayKey(owner)) === "true";
    sessionStorage.removeItem(replayKey(owner));
    return value;
  } catch {
    return false;
  }
}
// A shell remount during navigation must not lose the request; it is consumed
// only when the replayed tour finishes.
export function peekReplay(owner: string) {
  try {
    return sessionStorage.getItem(replayKey(owner)) === "true";
  } catch {
    return false;
  }
}
