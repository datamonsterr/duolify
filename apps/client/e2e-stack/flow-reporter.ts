// Writes flows.json next to the per-flow screenshots: one entry per flow spec with its
// steps, screenshot paths (relative to flows.json) and outcome. A later step turns each
// entry into a GitHub review issue.
import { writeFileSync, mkdirSync } from "node:fs";
import path from "node:path";
import type {
  Reporter,
  TestCase,
  TestResult,
  FullResult,
} from "@playwright/test/reporter";
import { live, shotsDir, stackURL } from "./stack";

type Entry = {
  id: string;
  title: string;
  spec: string;
  status:
    "passed" | "failed" | "fixme" | "skipped" | "timedOut" | "interrupted";
  reason?: string;
  error?: string;
  /** Product defects observed on the way that did not stop the flow (flow.defect). */
  defects: string[];
  durationMs: number;
  steps: { n: number; name: string; screenshot: string }[];
};

export default class FlowReporter implements Reporter {
  private flows = new Map<string, Entry>();

  onTestEnd(test: TestCase, result: TestResult) {
    // Runtime test.fixme(condition, reason) lands in result.annotations.
    const annotations = [...test.annotations, ...(result.annotations ?? [])];
    const declared = annotations.find((a) => a.type === "flow");
    const [id, ...rest] = (declared?.description ?? test.title).split(" ");
    const fixme = annotations.find((a) => a.type === "fixme");
    const steps = result.attachments
      .filter((a) => a.name.startsWith("flow-step:") && a.path)
      .map((a, i) => {
        // Playwright copies attachments into outputDir; point at the flow folder copy.
        const [, file, ...name] = a.name.split(":");
        return {
          n: i + 1,
          name: name.join(":"),
          screenshot: `${id}/${file}`,
        };
      });
    this.flows.set(id, {
      id,
      title: rest.join(" ") || test.title,
      spec: path.basename(test.location.file),
      status: result.status === "skipped" && fixme ? "fixme" : result.status,
      reason: fixme?.description,
      error: result.error?.message?.split("\n").slice(0, 6).join("\n"),
      defects: [
        ...new Set(
          annotations
            .filter((a) => a.type === "defect" && a.description)
            .map((a) => a.description!),
        ),
      ],
      durationMs: Math.round(result.duration),
      steps,
    });
  }

  onEnd(result: FullResult) {
    mkdirSync(shotsDir, { recursive: true });
    const manifest = {
      generatedAt: new Date().toISOString(),
      baseURL: stackURL,
      providerMode: live ? "live" : "mock",
      commit: process.env.GITHUB_SHA ?? null,
      status: result.status,
      flows: [...this.flows.values()].sort((a, b) =>
        a.id.localeCompare(b.id, undefined, { numeric: true }),
      ),
    };
    const file = path.join(shotsDir, "flows.json");
    writeFileSync(file, `${JSON.stringify(manifest, null, 2)}\n`);
    console.log(`flows manifest: ${file}`);
  }
}
