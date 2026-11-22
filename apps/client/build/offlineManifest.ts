// Which built files the service worker caches when. See public/lesson-shell-sw.js.
//   shell   - index.html plus everything the first screen loads; precached on install.
//   offline - route chunks an opened or downloaded lesson needs; cached when a lesson is opened
//             or a course is downloaded, so playback survives going offline.
// Everything else (other routes, posthog, Clerk) is cached on first use.
type Chunk = {
  type: "chunk";
  fileName: string;
  imports: string[];
  isEntry: boolean;
  isDynamicEntry: boolean;
  facadeModuleId: string | null;
  viteMetadata?: {
    importedCss: Set<string>;
    importedAssets: Set<string>;
  };
};
type Asset = { type: "asset"; fileName: string };
export type BundleLike = Record<string, Chunk | Asset>;

export const OFFLINE_ROUTE_MODULES = [
  "/features/lesson-player/LessonPlayer",
  "/features/lesson-player/immersive/ImmersiveUi",
  "/features/lessontest/LessonTest",
  "/features/design/Courses",
];
const APP_ENTRY = /\/index\.html$/;

function closure(bundle: BundleLike, roots: Chunk[]): Set<string> {
  const files = new Set<string>();
  const visit = (chunk: Chunk) => {
    if (files.has(chunk.fileName)) return;
    files.add(chunk.fileName);
    chunk.viteMetadata?.importedCss.forEach((f) => files.add(f));
    chunk.viteMetadata?.importedAssets.forEach((f) => files.add(f));
    for (const name of chunk.imports) {
      const next = bundle[name];
      if (next?.type === "chunk") visit(next);
    }
  };
  roots.forEach(visit);
  return files;
}

export function buildOfflineManifest(bundle: BundleLike): {
  shell: string[];
  offline: string[];
} {
  const chunks = Object.values(bundle).filter(
    (item): item is Chunk => item.type === "chunk",
  );
  const entries = chunks.filter(
    (c) => c.isEntry && c.facadeModuleId && APP_ENTRY.test(c.facadeModuleId),
  );
  if (entries.length === 0)
    throw new Error("offline manifest: app entry not found");
  const shell = closure(bundle, entries);
  // Fonts and images the stylesheets reference are tiny and needed to paint the shell.
  for (const item of Object.values(bundle))
    if (
      item.type === "asset" &&
      item.fileName.startsWith("assets/") &&
      !/\.(js|css|map)$/.test(item.fileName)
    )
      shell.add(item.fileName);
  const routeRoots = chunks.filter(
    (c) =>
      c.isDynamicEntry &&
      c.facadeModuleId &&
      OFFLINE_ROUTE_MODULES.some((m) => c.facadeModuleId!.includes(m)),
  );
  const offline = [...closure(bundle, routeRoots)].filter((f) => !shell.has(f));
  const url = (f: string) => `/${f}`;
  return {
    shell: ["/index.html", ...[...shell].map(url)],
    offline: offline.map(url),
  };
}
