import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import tokens from "../../../../docs/design/tokens.json";
import { Courses } from "../features/design/Courses";
import { server } from "./setup";
import { configureUiAnalytics } from "../lib/analytics";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));
const theme = readFileSync(
  resolve("src/test", "../../../../docs/design/theme.css"),
  "utf8",
);
function mount() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <Courses />
    </QueryClientProvider>,
  );
}
afterEach(() => {
  configureUiAnalytics(null);
  vi.clearAllMocks();
});

describe("green theme integration", () => {
  it("imports the canonical CSS and maps every light/dark semantic role without copying values", () => {
    const styles = readFileSync(resolve("src/test", "../styles.css"), "utf8");
    expect(styles).toContain('@import "../../../docs/design/theme.css"');
    for (const mode of ["light", "dark"] as const) {
      const block =
        mode === "light"
          ? theme.match(/:root\s*\{([^}]+)\}/)![1]
          : theme.match(/:root\[data-theme="dark"\]\s*\{([^}]+)\}/)![1];
      const sheet = document.createElement("style");
      sheet.textContent = `:root {${block}}`;
      document.head.append(sheet);
      for (const [name, token] of Object.entries(tokens.color[mode])) {
        expect(
          getComputedStyle(document.documentElement)
            .getPropertyValue(`--${name}`)
            .trim(),
        ).toBe(token.$value.hex);
        expect(theme).toContain(`--color-${name}: var(--${name})`);
      }
      sheet.remove();
    }
  });
  it("bundles all three licensed WOFF2 weights and retains Unicode fallbacks", () => {
    const fonts = readFileSync(
      resolve("src/test", "../features/design/fonts.css"),
      "utf8",
    );
    for (const weight of [600, 800, 900]) {
      expect(fonts).toContain(`font-weight: ${weight}`);
      const file = readFileSync(
        resolve(
          "src/test",
          `../assets/fonts/nunito-latin-${weight}-normal.woff2`,
        ),
      );
      expect(file.subarray(0, 4).toString()).toBe("wOF2");
    }
    expect(fonts).not.toMatch(/https?:/);
    expect(theme).toContain(
      '"Nunito", ui-rounded, ui-sans-serif, system-ui, sans-serif',
    );
    expect(
      readFileSync(resolve("src/test", "../assets/fonts/OFL.txt"), "utf8"),
    ).toContain("SIL OPEN FONT LICENSE");
  });
  it("composes card progress, accessible search, empty feedback and recovery using real API responses", async () => {
    mount();
    expect(
      await screen.findByRole("article", { name: "Everyday astronomy" }),
    ).toBeVisible();
    expect(screen.getAllByRole("progressbar")[0]).toHaveAttribute("value", "1");
    fireEvent.change(screen.getByRole("searchbox"), {
      target: { value: "private topic" },
    });
    expect(screen.getByText("No courses found")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
    expect(screen.getByRole("searchbox")).toHaveFocus();
    server.use(
      http.get("*/courses", () => HttpResponse.json({}, { status: 503 })),
    );
    fireEvent.click(screen.getByRole("button", { name: "Refresh courses" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not load courses",
    );
    expect(screen.getAllByRole("article")).toHaveLength(2);
    server.use(http.get("*/courses", () => HttpResponse.json([])));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("heading", {
        name: "What would you like to keep learning?",
      }),
    ).toBeVisible();
  });
  it("delivers only catalogued stable controls through the enabled PostHog adapter with UUID/common properties and opt-out", async () => {
    configureUiAnalytics({
      learnerId: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
      platform: "web",
      appVersion: "0.1.0",
      enabled: true,
    });
    mount();
    await screen.findByRole("article", { name: "Everyday astronomy" });
    fireEvent.change(screen.getByRole("searchbox"), {
      target: { value: "never send this learner text" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
    await waitFor(() =>
      expect(posthog.capture).toHaveBeenCalledWith("ui_control_activated", {
        distinct_id: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
        platform: "web",
        app_version: "0.1.0",
        source: "client",
        surface: "courses",
        control_id: "clear_search",
      }),
    );
    expect(JSON.stringify(posthog.capture.mock.calls)).not.toContain(
      "never send this learner text",
    );
    posthog.capture.mockClear();
    posthog.has_opted_out_capturing.mockReturnValue(true);
    fireEvent.click(screen.getByRole("button", { name: "Refresh courses" }));
    expect(posthog.capture).not.toHaveBeenCalled();
    posthog.has_opted_out_capturing.mockReturnValue(false);
  });
});

it("keeps shared dimensions, typography, motion, opacity and breakpoint mappings in parity", () => {
  const light = theme.match(/:root\s*\{([^}]+)\}/)![1];
  const declarations = Object.fromEntries(
    [...light.matchAll(/--([\w-]+):\s*([^;]+);/g)].map((match) => [
      match[1],
      match[2].trim(),
    ]),
  );
  for (const [group, values] of Object.entries({
    space: tokens.space,
    radius: tokens.radius,
    size: tokens.size,
  })) {
    for (const [name, token] of Object.entries(values)) {
      expect(declarations[`${group}-${name}`]).toBe(
        `${token.$value.value}${token.$value.unit}`,
      );
    }
  }
  for (const [name, token] of Object.entries(tokens.font.size))
    expect(declarations[`font-size-${name}`]).toBe(
      `${token.$value.value / 16}rem`,
    );
  for (const [group, values] of Object.entries({
    weight: tokens.font.weight,
    leading: tokens.font.leading,
    opacity: tokens.opacity,
  })) {
    for (const [name, token] of Object.entries(values))
      expect(declarations[`${group}-${name}`]).toBe(String(token.$value));
  }
  for (const [name, token] of Object.entries(tokens.font.tracking))
    expect(declarations[`tracking-${name}`]).toBe(
      `${token.$value.value}${token.$value.unit}`,
    );
  for (const [name, token] of Object.entries(tokens.motion.duration))
    expect(declarations[`duration-${name}`]).toBe(
      `${token.$value.value}${token.$value.unit}`,
    );
  for (const [name, token] of Object.entries(tokens.motion.easing))
    expect(declarations[`ease-${name}`]).toBe(
      `cubic-bezier(${token.$value.join(", ")})`,
    );
  for (const [name, token] of Object.entries(tokens.elevation))
    expect(declarations[`elevation-${name}`]).toBe(
      `0 ${token.$value.offsetY.value === 0 ? "0" : "var(--size-press-depth)"} 0 0 var(--primary-edge)`,
    );
  for (const [name, alias] of [
    ["phone", "sm"],
    ["sidebar", "md"],
    ["rail", "xl"],
  ] as const)
    expect(theme).toContain(
      `--breakpoint-${alias}: ${tokens.breakpoint[name].$value.value}rem`,
    );
  expect(declarations["font-family"]).toContain(tokens.font.family.$value[0]);
});
