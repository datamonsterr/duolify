import { describe, expect, it } from "vitest";
import { buildOfflineManifest, type BundleLike } from "./offlineManifest";

const chunk = (
  fileName: string,
  extra: Partial<Extract<BundleLike[string], { type: "chunk" }>> = {},
) => ({
  type: "chunk" as const,
  fileName,
  imports: [],
  isEntry: false,
  isDynamicEntry: false,
  facadeModuleId: null,
  ...extra,
});

describe("offline manifest", () => {
  const bundle: BundleLike = {
    "assets/app.js": chunk("assets/app.js", {
      isEntry: true,
      facadeModuleId: "/x/index.html",
      imports: ["assets/vendor.js"],
      viteMetadata: {
        importedCss: new Set(["assets/app.css"]),
        importedAssets: new Set(),
      },
    }),
    "assets/vendor.js": chunk("assets/vendor.js"),
    "assets/LessonPlayer.js": chunk("assets/LessonPlayer.js", {
      isDynamicEntry: true,
      facadeModuleId: "/x/src/features/lesson-player/LessonPlayer.tsx",
      imports: ["assets/vendor.js", "assets/playback.js"],
    }),
    "assets/playback.js": chunk("assets/playback.js"),
    "assets/ImmersiveUi.js": chunk("assets/ImmersiveUi.js", {
      isDynamicEntry: true,
      facadeModuleId: "/x/src/features/lesson-player/immersive/ImmersiveUi.tsx",
      imports: ["assets/vendor.js"],
      viteMetadata: {
        importedCss: new Set(["assets/ImmersiveUi.css"]),
        importedAssets: new Set(),
      },
    }),
    "assets/Marketplace.js": chunk("assets/Marketplace.js", {
      isDynamicEntry: true,
      facadeModuleId: "/x/src/features/sharing/MarketplacePage.tsx",
    }),
    "assets/font.woff2": { type: "asset", fileName: "assets/font.woff2" },
    "guided-chat.html": { type: "asset", fileName: "guided-chat.html" },
  };

  it("precaches the first screen only", () => {
    const { shell } = buildOfflineManifest(bundle);
    expect(shell).toEqual(
      expect.arrayContaining([
        "/index.html",
        "/assets/app.js",
        "/assets/vendor.js",
        "/assets/app.css",
        "/assets/font.woff2",
      ]),
    );
    expect(shell).not.toContain("/assets/LessonPlayer.js");
    expect(shell).not.toContain("/assets/Marketplace.js");
    expect(shell).not.toContain("/guided-chat.html");
  });

  it("lists the lesson chunks, not already in the shell, as the offline set", () => {
    const { offline } = buildOfflineManifest(bundle);
    expect(offline.sort()).toEqual([
      "/assets/ImmersiveUi.css",
      "/assets/ImmersiveUi.js",
      "/assets/LessonPlayer.js",
      "/assets/playback.js",
    ]);
  });
});
