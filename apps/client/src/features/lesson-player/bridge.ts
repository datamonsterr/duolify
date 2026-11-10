export type SlideCommand =
  | { type: "play" | "pause" }
  | { type: "seek"; time: number }
  | { type: "tick"; time: number }
  | { type: "rate"; rate: number }
  | { type: "set_motion"; reduced: boolean }
  | { type: "set_expression"; speaker?: string; expression: string };
export type SlideMessage =
  | { type: "ended" }
  | { type: "checkpoint"; name: string }
  | {
      type: "character_rendered";
      speaker_key: string;
      character_version: number;
      rig_version: string;
      expression: string;
      offline: boolean;
      slide_id?: string;
    }
  | {
      type: "character_render_fallback";
      reason_code: string;
      offline: boolean;
    }
  | { type: "player_keydown"; code: string; key: string }
  | { type: "asset_error" };
export function isSlideMessage(
  event: MessageEvent,
  frame: Window | null,
  checkpoints?: readonly string[],
): event is MessageEvent<SlideMessage> {
  if (
    !frame ||
    event.source !== frame ||
    event.origin !== "null" ||
    !event.data ||
    typeof event.data !== "object"
  )
    return false;
  return (
    (event.data.type === "ended" && Object.keys(event.data).length === 1) ||
    (event.data.type === "asset_error" &&
      Object.keys(event.data).length === 1) ||
    (event.data.type === "checkpoint" &&
      Object.keys(event.data).length === 2 &&
      typeof event.data.name === "string" &&
      /^[A-Za-z0-9_-]{1,100}$/.test(event.data.name) &&
      (checkpoints === undefined || checkpoints.includes(event.data.name))) ||
    (event.data.type === "character_rendered" &&
      typeof event.data.speaker_key === "string" &&
      typeof event.data.character_version === "number" &&
      typeof event.data.rig_version === "string" &&
      typeof event.data.expression === "string" &&
      typeof event.data.offline === "boolean") ||
    (event.data.type === "character_render_fallback" &&
      typeof event.data.reason_code === "string" &&
      typeof event.data.offline === "boolean") ||
    (event.data.type === "player_keydown" &&
      typeof event.data.code === "string" &&
      typeof event.data.key === "string")
  );
}
export function nextSlide(index: number, count: number) {
  return Math.min(index + 1, count - 1);
}
export function sendCommand(frame: Window | null, command: SlideCommand) {
  frame?.postMessage(command, "*");
}

type BridgeConfig = {
  duration: number;
  checkpoints: { id: string; at_ms: number }[];
};
// Installed ahead of generated markup. Only the host can control this frame;
// its parent must still validate every incoming event against the active frame.
function motionBridge(config: BridgeConfig) {
  let playing = false,
    rate = 1,
    time = 0,
    ended = false;
  const seen = new Set<string>();
  const apply = () => {
    for (const animation of document.getAnimations()) {
      animation.pause();
      animation.playbackRate = rate;
      animation.currentTime = time;
      if (playing && document.documentElement.dataset.motion !== "reduce")
        animation.play();
    }
  };
  window.addEventListener(
    "error",
    (event) => {
      const target = event.target;
      if (
        target &&
        (target instanceof HTMLImageElement ||
          target instanceof HTMLMediaElement ||
          target instanceof HTMLLinkElement ||
          target instanceof HTMLScriptElement)
      ) {
        parent.postMessage({ type: "asset_error" }, "*");
      }
    },
    true,
  );
  document.addEventListener("DOMContentLoaded", apply);
  window.addEventListener("message", (event) => {
    if (
      event.source !== parent ||
      !event.data ||
      typeof event.data !== "object" ||
      Array.isArray(event.data)
    )
      return;
    const value = event.data;
    const keys = Object.keys(value).length;
    if ((value.type === "play" || value.type === "pause") && keys === 1)
      playing = value.type === "play";
    else if (
      (value.type === "seek" || value.type === "tick") &&
      keys === 2 &&
      Number.isFinite(value.time) &&
      value.time >= 0 &&
      value.time <= config.duration
    ) {
      time = value.time;
      if (value.type === "seek") {
        ended = false;
        seen.clear();
        config.checkpoints
          .filter((c) => c.at_ms < time)
          .forEach((c) => seen.add(c.id));
      } else {
        for (const checkpoint of config.checkpoints) {
          if (time >= checkpoint.at_ms && !seen.has(checkpoint.id)) {
            seen.add(checkpoint.id);
            parent.postMessage(
              { type: "checkpoint", name: checkpoint.id },
              "*",
            );
          }
        }
        if (time === config.duration && !ended) {
          ended = true;
          playing = false;
          parent.postMessage({ type: "ended" }, "*");
        }
      }
    } else if (
      value.type === "rate" &&
      keys === 2 &&
      [0.75, 1, 1.25, 1.5, 2].includes(value.rate)
    )
      rate = value.rate;
    else if (
      value.type === "set_motion" &&
      keys === 2 &&
      typeof value.reduced === "boolean"
    )
      document.documentElement.dataset.motion = value.reduced
        ? "reduce"
        : "full";
    else return;
    apply();
    // Authored JS still receives the protocol. Reconcile CSS/Web Animations
    // after its handlers so a legacy seek handler cannot restart a paused slide.
    queueMicrotask(apply);
  });
}
export function bridgeScript(config: BridgeConfig) {
  return `<script>(${motionBridge.toString()})(${JSON.stringify(config)})</script>`;
}
