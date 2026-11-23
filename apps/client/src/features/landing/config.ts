// The only place that names landing media. The poster and captions are small and ship in
// public/landing (web origin). The video and WebM are not in git and not in the Tauri bundle:
// they are served from the asset origin at VITE_LANDING_MEDIA_BASE (infra:landing-media-upload).
// Without the base the landing shows the poster and no player.
const base = (import.meta.env.VITE_LANDING_MEDIA_BASE ?? "").replace(/\/$/, "");
export const landingMedia = {
  mp4: base ? `${base}/landing.mp4` : "",
  webm: base ? `${base}/landing.webm` : "",
  poster: "/landing/landing-poster-960.webp",
  captionsEn: "/landing/landing.en.vtt",
} as const;
