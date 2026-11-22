/**
 * A posthog-shaped facade that keeps posthog-js out of the entry chunk.
 *
 * - The SDK is imported and initialised after first paint, and only once consent has been
 *   resolved to "opted in" (the first `opt_in_capturing`). Before that nothing touches the network.
 * - Events captured while the SDK is loading are queued and replayed in order.
 * - `opt_out_capturing` drops everything queued and cancels a pending load: opt-out stays opt-out.
 * - Consent is tracked locally too, so `has_opted_out_capturing` is right before the SDK exists.
 */
export type PosthogLike = {
  opt_in_capturing(): void;
  opt_out_capturing(): void;
  has_opted_out_capturing(): boolean;
  identify(distinctId: string): void;
  capture(event: string, properties?: Record<string, unknown>): void;
};

type Op =
  | { kind: "opt_in" }
  | { kind: "opt_out" }
  | { kind: "identify"; id: string }
  | { kind: "capture"; event: string; properties?: Record<string, unknown> };

export type LazyPosthogOptions = {
  /** Imports the SDK. */
  load: () => Promise<PosthogLike>;
  /** Runs once, right after the SDK arrives and before the queue is replayed. */
  init?: (sdk: PosthogLike) => void;
  /** Calls `run` after first paint, when the browser is idle. */
  schedule: (run: () => void) => void;
  /** False when no project key is configured: nothing is queued or loaded. */
  enabled: boolean;
  /** An SDK that is already available (tests); skips load and init. */
  preloaded?: PosthogLike;
  maxQueue?: number;
};

export function createLazyPosthog(options: LazyPosthogOptions): PosthogLike {
  const max = options.maxQueue ?? 200;
  let sdk: PosthogLike | undefined = options.preloaded;
  let optedOut = false;
  let wantLoad = false;
  let scheduled = false;
  let failed = false;
  let queue: Op[] = [];

  const apply = (target: PosthogLike, op: Op) => {
    try {
      if (op.kind === "opt_in") target.opt_in_capturing();
      else if (op.kind === "opt_out") target.opt_out_capturing();
      else if (op.kind === "identify") target.identify(op.id);
      else target.capture(op.event, op.properties);
    } catch {
      /* Analytics is derived data and never blocks the app. */
    }
  };
  const flush = (target: PosthogLike) => {
    const ops = queue;
    queue = [];
    for (const op of ops) apply(target, op);
  };
  const startLoad = () => {
    if (sdk || scheduled || failed || !options.enabled) return;
    scheduled = true;
    options.schedule(() => {
      if (!wantLoad || optedOut) {
        scheduled = false;
        return;
      }
      options
        .load()
        .then((loaded) => {
          if (optedOut) {
            // Opted out while the SDK was in flight: never initialise it.
            scheduled = false;
            return;
          }
          options.init?.(loaded);
          sdk = loaded;
          flush(loaded);
        })
        .catch(() => {
          failed = true;
          queue = [];
        });
    });
  };
  const push = (op: Op) => {
    if (failed || !options.enabled) return;
    queue.push(op);
    if (queue.length > max) queue.shift();
  };

  return {
    opt_in_capturing() {
      optedOut = false;
      if (sdk) return apply(sdk, { kind: "opt_in" });
      if (!options.enabled) return;
      wantLoad = true;
      push({ kind: "opt_in" });
      startLoad();
    },
    opt_out_capturing() {
      optedOut = true;
      wantLoad = false;
      if (sdk) return apply(sdk, { kind: "opt_out" });
      // Nothing queued survives an opt-out; the SDK learns of it if it ever loads.
      queue = [];
      push({ kind: "opt_out" });
    },
    has_opted_out_capturing() {
      if (sdk) {
        try {
          return sdk.has_opted_out_capturing();
        } catch {
          return optedOut;
        }
      }
      return optedOut;
    },
    identify(id) {
      if (sdk) return apply(sdk, { kind: "identify", id });
      if (optedOut) return;
      push({ kind: "identify", id });
    },
    capture(event, properties) {
      if (sdk) return apply(sdk, { kind: "capture", event, properties });
      if (optedOut) return;
      push({ kind: "capture", event, properties });
    },
  };
}

/** After first paint: two frames (paint committed), then idle, with a ceiling for hidden tabs. */
export function afterFirstPaint(run: () => void): void {
  let done = false;
  const once = () => {
    if (done) return;
    done = true;
    run();
  };
  const idle = () => {
    const ric = (
      globalThis as {
        requestIdleCallback?: (cb: () => void, o?: { timeout: number }) => void;
      }
    ).requestIdleCallback;
    if (ric) ric(once, { timeout: 2000 });
    else setTimeout(once, 1);
  };
  if (typeof requestAnimationFrame === "function")
    requestAnimationFrame(() => requestAnimationFrame(idle));
  else idle();
  // rAF never fires in a background tab.
  setTimeout(once, 5000);
}
