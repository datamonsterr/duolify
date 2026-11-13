import { session } from "../accounts/session";
import type { ManifestSlide } from "../player/manifest";

export const NARRATION_CACHE_NAME = "duolify-narration-v1";

/**
 * Fetches narration audio and alignment assets, utilizing the Cache API for offline replay.
 */
export async function fetchNarrationAssets(
  audioUrl: string,
  alignmentUrl: string,
): Promise<{ rawAlignment: unknown; audioBlob: Blob }> {
  let cachedAudio: Response | undefined;
  let cachedAlignment: Response | undefined;

  if (typeof caches !== "undefined") {
    try {
      const cache = await caches.open(NARRATION_CACHE_NAME);
      const [a, b] = await Promise.all([
        cache.match(alignmentUrl),
        cache.match(audioUrl),
      ]);
      cachedAlignment = a;
      cachedAudio = b;
    } catch {
      // Cache match errors fail open to network
    }
  }

  if (cachedAlignment && cachedAudio) {
    try {
      const [rawAlignment, audioBlob] = await Promise.all([
        cachedAlignment.json(),
        cachedAudio.blob(),
      ]);
      return { rawAlignment, audioBlob };
    } catch {
      // Fall through to network on corrupt cache entries
    }
  }

  // Fetch over network
  const [rawAlignment, audioBlob] = await Promise.all([
    session.request<unknown>(alignmentUrl),
    session.blob(audioUrl),
  ]);

  if (typeof caches !== "undefined") {
    try {
      const cache = await caches.open(NARRATION_CACHE_NAME);
      await Promise.all([
        cache.put(
          alignmentUrl,
          new Response(JSON.stringify(rawAlignment), {
            headers: { "Content-Type": "application/json" },
          }),
        ),
        cache.put(
          audioUrl,
          new Response(audioBlob, {
            headers: { "Content-Type": "audio/wav" },
          }),
        ),
      ]);
    } catch {
      // Cache write error is non-fatal (disposable cache)
    }
  }

  return { rawAlignment, audioBlob };
}

/**
 * Preloads audio and alignment assets for upcoming slides in the background.
 */
export async function preloadNarration(
  slide: ManifestSlide | undefined,
): Promise<void> {
  if (!slide || slide.narration_status !== "ready" || !slide.narration) return;
  try {
    await fetchNarrationAssets(
      slide.narration.audio_url,
      slide.narration.alignment_url,
    );
  } catch {
    // Background preload fails silently
  }
}
