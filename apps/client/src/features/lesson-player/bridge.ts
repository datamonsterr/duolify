export type SlideCommand =
  { type: "play" | "pause" } | { type: "seek"; time: number };
export type SlideMessage =
  { type: "ended" } | { type: "checkpoint"; name: string };
export function isSlideMessage(
  event: MessageEvent,
  frame: Window | null,
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
    event.data.type === "ended" ||
    (event.data.type === "checkpoint" && typeof event.data.name === "string")
  );
}
export function nextSlide(index: number, count: number) {
  return Math.min(index + 1, count - 1);
}
export function sendCommand(frame: Window | null, command: SlideCommand) {
  frame?.postMessage(command, "*");
}
