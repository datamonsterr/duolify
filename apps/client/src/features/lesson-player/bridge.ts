export type SlideCommand =
  | { type: "play" | "pause" }
  | { type: "seek"; time: number }
  | { type: "rate"; rate: number }
  | { type: "set_motion"; reduced: boolean };
export type SlideMessage =
  { type: "ended" } | { type: "checkpoint"; name: string };
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
    (event.data.type === "checkpoint" &&
      Object.keys(event.data).length === 2 &&
      typeof event.data.name === "string" &&
      /^[A-Za-z0-9_-]{1,100}$/.test(event.data.name) &&
      (checkpoints === undefined || checkpoints.includes(event.data.name)))
  );
}
export function nextSlide(index: number, count: number) {
  return Math.min(index + 1, count - 1);
}
export function sendCommand(frame: Window | null, command: SlideCommand) {
  frame?.postMessage(command, "*");
}
