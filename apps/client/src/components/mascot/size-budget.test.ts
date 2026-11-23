import { build } from "vite";
import { describe, expect, it } from "vitest";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
export const BUDGET_BYTES = 30 * 1024;

describe("mascot size budget", () => {
  it("code, styles and art total at most 30 KB minified", async () => {
    const out = (await build({
      configFile: false,
      mode: "production",
      logLevel: "silent",
      root: here,
      plugins: [(await import("@vitejs/plugin-react")).default()],
      build: {
        write: false,
        minify: true,
        cssMinify: true,
        lib: {
          entry: path.join(here, "index.ts"),
          formats: ["iife"],
          name: "M",
          fileName: "m",
          cssFileName: "m",
        },
        rollupOptions: {
          external: (id) =>
            /^react(\/|$)|react-dom|posthog|clerk|tanstack|tauri/.test(id) ||
            /features\/|lib\//.test(id),
        },
      },
    })) as {
      output: {
        fileName: string;
        type: string;
        code?: string;
        source?: string | Uint8Array;
      }[];
    }[];
    const files = out.flatMap((o) => o.output);
    if (process.env.DUMP)
      (await import("node:fs")).writeFileSync(
        process.env.DUMP,
        String(files[0].code),
      );
    const bytes = files.reduce(
      (n, f) =>
        n + Buffer.byteLength(String(f.type === "chunk" ? f.code : f.source)),
      0,
    );
    expect(
      bytes,
      files
        .map(
          (f) =>
            `${f.fileName}:${Buffer.byteLength(String(f.type === "chunk" ? f.code : f.source))}`,
        )
        .join(),
    ).toBeLessThanOrEqual(BUDGET_BYTES);
    console.info("mascot minified bytes", bytes);
  }, 60_000);
});
