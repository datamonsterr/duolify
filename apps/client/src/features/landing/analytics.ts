import { appVersion } from "../../lib/client-build";
import { posthog } from "../../lib/posthog";

// Landing page (openspec/changes/landing-page): anonymous, no identity, no free text. Sent
// only while the visitor has not opted out of analytics.
export function trackLanding(
  event:
    | "landing_viewed"
    | "landing_video_played"
    | "landing_video_completed"
    | "landing_cta_clicked",
  properties: { target?: string } = {},
) {
  try {
    if (localStorage.getItem("duolify.analytics-opt-out") === "true") return;
    if (posthog.has_opted_out_capturing()) return;
    posthog.capture(event, {
      ...properties,
      platform: "web",
      app_version: appVersion,
      source: "client",
    });
  } catch {
    /* Analytics never blocks the landing page. */
  }
}
