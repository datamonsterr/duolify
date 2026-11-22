import { useLayoutEffect, useRef, type IframeHTMLAttributes } from "react";

/**
 * Real URL of the sandbox host page (public/lesson-frame.html); the edge gives it its own CSP header.
 * The query is the build id. The edge serves the page `immutable`, so each build has its own URL, and a
 * lesson opened or downloaded online replays offline from the HTTP cache: a sandboxed frame has an opaque
 * origin, which Chromium never routes through the service worker (public/lesson-shell-sw.js only primes
 * the cache with this URL).
 */
export const LESSON_FRAME_URL = `/lesson-frame.html?v=${__BUILD_ID__}`;

type Props = Omit<
  IframeHTMLAttributes<HTMLIFrameElement>,
  "src" | "srcDoc" | "sandbox" | "onLoad"
> & {
  //** Full slide document (see sandboxDocument). Never rendered into the app's own document. */
  html: string;
  /** Called once the slide document is in the frame and its scripts have started. */
  onLoad?: () => void;
  ref?: React.Ref<HTMLIFrameElement>;
};

/**
 * Plays a slide in `<iframe sandbox="allow-scripts">`, never with allow-same-origin. A `srcdoc` frame inherits the
 * embedding page's Content-Security-Policy, which would force `unsafe-inline` onto the whole app, so the slide is
 * loaded into a same-origin host page that carries its own policy: the frame announces itself, receives the
 * document by postMessage and writes it. Changing `html` reloads the frame.
 */
export function SandboxFrame({ html, onLoad, ref, ...attributes }: Props) {
  const frame = useRef<HTMLIFrameElement | null>(null);
  const latest = useRef({ html, onLoad });
  const first = useRef(true);
  useLayoutEffect(() => {
    latest.current = { html, onLoad };
  });
  // Layout effect: the listener must exist before the frame can run its first script.
  useLayoutEffect(() => {
    const listener = (event: MessageEvent) => {
      const target = frame.current?.contentWindow;
      if (!target || event.source !== target) return;
      const type = (event.data as { type?: unknown } | null)?.type;
      if (type === "frame_ready")
        target.postMessage(
          { type: "load_document", html: latest.current.html },
          "*",
        );
      else if (type === "document_loaded") latest.current.onLoad?.();
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }, []);
  useLayoutEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    // Same URL, new slide: navigating again resets the frame, as changing srcdoc did.
    if (frame.current) frame.current.src = LESSON_FRAME_URL;
  }, [html]);
  return (
    <iframe
      {...attributes}
      ref={(node) => {
        frame.current = node;
        if (typeof ref === "function") ref(node);
        else if (ref) ref.current = node;
      }}
      src={LESSON_FRAME_URL}
      sandbox="allow-scripts"
    />
  );
}
