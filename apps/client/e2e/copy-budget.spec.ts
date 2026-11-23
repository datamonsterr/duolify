// Per-screen visible-string counter (openspec simplify-ux-gestures-hotkeys, task 1.1).
// Counts distinct visible text nodes at 390x844 in en and ar (rtl), checks heading, label and
// caption limits, and compares with e2e/copy-budget.baseline.json, which may only go down.
// Run with COPY_BUDGET_UPDATE=1 to lower the baseline after a screen pass.
import { readFileSync, writeFileSync } from "node:fs";
import { expect, test, type Page } from "./native-store-fixture";

const baselineFile = new URL("./copy-budget.baseline.json", import.meta.url);
type Measure = {
  strings: number;
  headings: number;
  labels: number;
  captions: number;
};
const baseline: Record<string, Measure> = JSON.parse(
  readFileSync(baselineFile, "utf8"),
);
const update = process.env.COPY_BUDGET_UPDATE === "1";
const measured: Record<string, Measure> = {};

const screens = [
  ["learn", "/"],
  ["courses", "/courses"],
  ["marketplace", "/marketplace"],
  ["credits", "/credits"],
  ["notes", "/notes"],
  ["bookmarks", "/bookmarks"],
  ["settings", "/settings"],
  ["friends", "/friends"],
  ["leagues", "/leagues"],
  ["more", "/more"],
] as const;

async function seed(page: Page, locale: string) {
  await page.addInitScript(() =>
    sessionStorage.setItem(
      "duolify.native-store-fixture",
      JSON.stringify({
        access_token: "copy-access",
        refresh_token: "copy-refresh",
        expires_in: 900,
      }),
    ),
  );
  await page.route("**/v1/**", (route) => route.fulfill({ json: [] }));
  await page.route("**/v1/me", (route) =>
    route.fulfill({
      json: {
        id: "10000000-0000-4000-8000-000000000001",
        display_name: "Learner",
        locale,
        time_zone: "UTC",
        background: "",
        analytics_opt_out: true,
        welcome_state: "completed",
        tour_state: "completed",
      },
    }),
  );
}

// Runs in the page: distinct visible text nodes outside [data-user-content], by role.
function measureVisible(arFactor: number): Measure {
  const seen = new Map<string, string>();
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const text = (n.textContent ?? "").replace(/\s+/g, " ").trim();
    const el = n.parentElement;
    if (!text || !el || !/\p{L}/u.test(text)) continue;
    if (el.closest("script,style,noscript,[data-user-content]")) continue;
    if (
      !el.checkVisibility({ visibilityProperty: true, opacityProperty: true })
    )
      continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) continue;
    const role = el.closest("h1,h2,h3,[role=heading]")
      ? "heading"
      : el.closest("button,[role=tab],[role=button]")
        ? "label"
        : el.closest("p,small,figcaption,[role=status]")
          ? "caption"
          : "text";
    if (!seen.has(text)) seen.set(text, role);
  }
  const words = (s: string) => s.split(/\s+/).length;
  const chars = (s: string) => [...s].length;
  const out: Measure = {
    strings: seen.size,
    headings: 0,
    labels: 0,
    captions: 0,
  };
  for (const [text, role] of seen) {
    if (role === "heading" && (words(text) > 4 || chars(text) > 24 * arFactor))
      out.headings++;
    if (role === "label" && (words(text) > 2 || chars(text) > 16 * arFactor))
      out.labels++;
    if (role === "caption" && chars(text) > 60 * arFactor) out.captions++;
  }
  return out;
}

for (const locale of ["en", "ar"]) {
  for (const [name, path] of screens) {
    test(`copy budget ${name} (${locale})`, async ({ page }) => {
      await page.setViewportSize({ width: 390, height: 844 });
      await seed(page, locale);
      await page.goto(path);
      if (locale === "ar")
        await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
      await page.locator("main, [role=main], #root > *").first().waitFor();
      await page.waitForLoadState("networkidle");
      const m = await page.evaluate(measureVisible, locale === "ar" ? 1.3 : 1);
      const key = `${name}.${locale}`;
      measured[key] = m;
      if (update) return;
      const allowed = baseline[key];
      expect(
        allowed,
        `no baseline for ${key}; run with COPY_BUDGET_UPDATE=1`,
      ).toBeTruthy();
      for (const k of ["strings", "headings", "labels", "captions"] as const)
        expect(
          m[k],
          `${key} ${k}: ${m[k]} visible exceeds the baseline ${allowed[k]}`,
        ).toBeLessThanOrEqual(allowed[k]);
    });
  }
}

test.afterAll(() => {
  if (!update) return;
  const merged = { ...baseline };
  for (const [k, v] of Object.entries(measured)) merged[k] = v;
  writeFileSync(baselineFile, JSON.stringify(merged, null, 2) + "\n");
});
