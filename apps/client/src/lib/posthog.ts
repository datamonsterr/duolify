import { afterFirstPaint, createLazyPosthog } from "./lazyPosthog";

const key = import.meta.env.VITE_POSTHOG_KEY;

// Unit tests mock the SDK module and assert on it synchronously, so they get it up front. The
// branch is a build-time constant: production bundles load the SDK lazily from a separate chunk.
const preloaded =
  import.meta.env.MODE === "test"
    ? (await import("posthog-js/dist/module.no-external")).default
    : undefined;

/** The app's only PostHog handle. Same calls as posthog-js; see lazyPosthog for the loading rules. */
export const posthog = createLazyPosthog({
  enabled: Boolean(key) || preloaded !== undefined,
  preloaded,
  schedule: afterFirstPaint,
  load: async () =>
    (await import("posthog-js/dist/module.no-external")).default,
  init: (sdk) => {
    (
      sdk as unknown as {
        init(key: string, config: Record<string, unknown>): void;
      }
    ).init(key, {
      api_host: import.meta.env.VITE_POSTHOG_HOST || "https://us.i.posthog.com",
      defaults: "2026-08-30",
      autocapture: false,
      capture_pageview: false,
    });
  },
});
