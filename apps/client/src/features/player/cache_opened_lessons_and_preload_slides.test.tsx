import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { playerManifest, playerHTML } from "../../mocks/player";
import { session } from "../accounts/session";
import {
  downloadLesson,
  readCachedLesson,
  clearLessonCache,
  digest,
  materialize,
} from "./cache";
import {
  configureAnalytics,
  trackPlayerRecovery,
  trackPlayer,
} from "../../lib/analytics";
import posthog from "posthog-js/dist/module.no-external";
vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: () => false,
    opt_out_capturing: vi.fn(),
    opt_in_capturing: vi.fn(),
  },
}));
const owner = "a1111111-1111-4111-8111-111111111111";
const entries = new Map<string, Response>();
let quota = false;
beforeEach(() => {
  vi.stubGlobal("crypto", webcrypto);
  vi.stubGlobal("caches", {
    open: async () => ({
      match: async (key: string) => entries.get(String(key))?.clone(),
      put: async (key: string, response: Response) => {
        if (quota) throw new Error("quota");
        entries.set(String(key), response.clone());
      },
      keys: async () => [...entries.keys()].map((url) => new Request(url)),
      delete: async (key: string | Request) =>
        entries.delete(typeof key === "string" ? key : key.url),
    }),
    keys: async () => ["duolify-player-v1"],
    delete: async () => {
      entries.clear();
      return true;
    },
  });
  vi.spyOn(session, "current").mockReturnValue({
    id: owner,
    display_name: "",
    locale: "en",
    time_zone: "UTC",
    background: "",
    analytics_opt_out: false,
  });
  vi.spyOn(session, "request").mockResolvedValue({
    html: playerHTML,
    assets: [],
  });
});
afterEach(async () => {
  quota = false;
  await clearLessonCache();
  entries.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  configureAnalytics(null);
});
async function manifest(version = 1) {
  const m = playerManifest();
  m.version = version;
  return {
    ...m,
    slides: await Promise.all(
      m.slides.map(async (s) => ({
        ...s,
        html_sha256: await digest(playerHTML),
        asset_sha256: {},
      })),
    ),
  };
}
describe("opened lesson atomic cache", () => {
  it("persists every slide for a fresh offline read and scopes accounts", async () => {
    const m = await manifest();
    await downloadLesson(m, owner);
    vi.mocked(session.request).mockRejectedValue(new TypeError("offline"));
    const saved = await readCachedLesson(owner, "sky");
    expect(saved?.manifest).toEqual(m);
    for (const slide of m.slides)
      expect(materialize(saved!, slide)).toContain("Look up");
    expect(await readCachedLesson("other", "sky")).toBeNull();
  });
  it("preserves a valid version after partial, checksum and quota failures", async () => {
    await downloadLesson(await manifest(), owner);
    vi.mocked(session.request).mockRejectedValueOnce(
      new TypeError("interrupted"),
    );
    await expect(downloadLesson(await manifest(2), owner)).rejects.toThrow();
    vi.mocked(session.request).mockResolvedValueOnce({
      html: "corrupt",
      assets: [],
    });
    await expect(downloadLesson(await manifest(2), owner)).rejects.toThrow(
      "checksum_mismatch",
    );
    quota = true;
    await expect(downloadLesson(await manifest(2), owner)).rejects.toThrow();
    quota = false;
    expect((await readCachedLesson(owner, "sky"))?.manifest.version).toBe(1);
  });
  it("does not recreate private cache after logout during download", async () => {
    let release!: (v: unknown) => void;
    vi.mocked(session.request).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    const pending = downloadLesson(await manifest(), owner);
    await vi.waitFor(() => expect(release).toBeDefined());
    await clearLessonCache();
    release({ html: playerHTML, assets: [] });
    await expect(pending).rejects.toThrow();
    expect(entries.size).toBe(0);
  });
  it("bounds lessons and fails closed after corruption or eviction", async () => {
    for (let n = 0; n < 6; n++) {
      const m = await manifest();
      m.lesson_id = `lesson-${n}`;
      m.slides.forEach(
        (s) => (s.html_url = `/v1/lessons/${m.lesson_id}/slides/${s.slide_id}`),
      );
      await downloadLesson(m, owner);
    }
    expect(entries.size).toBe(5);
    expect(await readCachedLesson(owner, "lesson-0")).toBeNull();
    entries.clear();
    expect(await readCachedLesson(owner, "lesson-5")).toBeNull();
  });
  it("emits only registered recovery codes with UUID/common properties and consent", () => {
    configureAnalytics({
      learnerId: owner,
      optedOut: false,
      platform: "web",
      appVersion: "0.1.0",
    });
    trackPlayerRecovery("retry");
    trackPlayer("lesson_player_error", {
      lesson_id: "sky",
      error_code: "offline_uncached",
    });
    expect(posthog.capture).toHaveBeenCalledWith("ui_recovery_selected", {
      distinct_id: owner,
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      surface: "lesson_player",
      error_code: "offline_uncached",
      action: "retry",
    });
    configureAnalytics({ learnerId: owner, optedOut: true, platform: "web" });
    trackPlayerRecovery("exit");
    expect(posthog.capture).toHaveBeenCalledTimes(2);
  });
});

it("contains dynamic HTML without a network origin and scopes prepared documents to the owner", async () => {
  const { AuthError } = await import("../accounts/session");
  const { openLesson, preparedDocument } = await import("./cache");
  vi.mocked(session.request)
    .mockRejectedValueOnce(new AuthError("missing", 404))
    .mockResolvedValueOnce({
      id: "dynamic",
      course_id: "course",
      slides: [
        {
          slide_id: "s1",
          html: "<p>Private dynamic lesson</p>",
          duration_ms: 1000,
          narration: "",
        },
      ],
    });
  const m = await openLesson("dynamic");
  expect(m.asset_origin).toBe("");
  expect(preparedDocument(m.slides[0])).toContain("Private dynamic lesson");
  vi.mocked(session.current).mockReturnValue({
    ...session.current()!,
    id: "other",
  });
  expect(preparedDocument(m.slides[0])).toBeUndefined();
  vi.mocked(session.current).mockReturnValue({
    ...session.current()!,
    id: owner,
  });
  session.clear();
  expect(preparedDocument(m.slides[0])).toBeUndefined();
});

it("does not republish a dynamic document after logout during retrieval", async () => {
  const { AuthError } = await import("../accounts/session");
  const { openLesson, preparedDocument } = await import("./cache");
  const slide = (await manifest()).slides[0];
  vi.mocked(session.request)
    .mockRejectedValueOnce(new AuthError("missing", 404))
    .mockImplementationOnce(async () => {
      session.clear();
      return {
        id: "sky",
        slides: [
          {
            slide_id: slide.slide_id,
            html: "late private HTML",
            duration_ms: 1000,
          },
        ],
      };
    });
  await expect(openLesson("sky")).rejects.toThrow();
  expect(preparedDocument(slide)).toBeUndefined();
});
