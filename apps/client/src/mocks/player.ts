// Disposable host fixture for component tests; browser acceptance uses the API.
export function playerManifest(lessonId = "sky") {
  return {
    lesson_id: lessonId,
    course_id: "astronomy",
    title: "Why the sky changes",
    version: 1,
    asset_origin: "https://assets.example.test",
    slides: [1, 2, 3].map((n) => ({
      slide_id: `sky-${n}`,
      html_sha256:
        "a58876eb52bda6f6cca061b179aa66c358d8276647ffaba91b2201ae480eb9c2",
      asset_sha256: {},
      duration_ms: 3000,
      html_url: `/v1/lessons/${lessonId}/slides/sky-${n}`,
      assets: [],
      checkpoints: [],
      narration_status: "disabled" as const,
      narration_text: [
        "The sky is a map waiting to be explored.",
        "Stars are distant suns, shining across space.",
        "Notice how the sky changes as Earth turns.",
      ][n - 1],
    })),
  };
}
export const playerHTML =
  '<html><head></head><body><h1>Look up</h1><div class="star">✦</div></body></html>';
