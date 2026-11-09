import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, expect, it, vi } from "vitest";
import approved from "./fixtures/approved.json";
import { PricingView } from "./PricingView";
import { formatCredits, formatMoney } from "./format";
import { readPricingCache } from "../../lib/api";

beforeEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

it("formats catalogue amounts with locale-aware number and currency rules", () => {
  expect(formatCredits(1600, "en-US")).toBe("1,600");
  expect(formatMoney(999, "USD", "en-US")).toBe("$9.99");
  expect(formatMoney(999, "EUR", "de-DE")).toContain("9,99");
});

it("discards malformed cached catalogues", () => {
  sessionStorage.setItem(
    "pricing:last-known",
    JSON.stringify({
      pricing: { version: "broken", packs: [null] },
      etag: "bad",
    }),
  );
  expect(readPricingCache()).toBeUndefined();
});

it("revalidates with ETag, refreshes a flipped version, and shows it as last known offline", async () => {
  let version = "flip-a";
  let offline = false;
  const fetchSpy = vi
    .spyOn(globalThis, "fetch")
    .mockImplementation(async (_input, init) => {
      const headers = new Headers(init?.headers);
      if (offline) throw new TypeError("offline");
      if (headers.get("If-None-Match") === '"flip-a"' && version === "flip-a")
        return new Response(null, { status: 304 });
      const catalogue = structuredClone(approved);
      catalogue.version = version;
      catalogue.actions.preview.credits = version === "flip-a" ? 25 : 31;
      return new Response(JSON.stringify(catalogue), {
        status: 200,
        headers: { ETag: `"${version}"`, "Content-Type": "application/json" },
      });
    });
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <PricingView />
    </QueryClientProvider>,
  );

  fireEvent.click(
    await screen.findByRole("button", { name: "Course creation costs" }),
  );
  expect(await screen.findByText("25 credits", { exact: true })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Refresh prices" }));
  await screen.findByText("25 credits", { exact: true });
  expect(
    new Headers(fetchSpy.mock.calls[1][1]?.headers).get("If-None-Match"),
  ).toBe('"flip-a"');

  await screen
    .findByRole("button", { name: "Refresh prices" })
    .then((button) => expect(button).toBeEnabled());
  version = "flip-b";
  fireEvent.click(screen.getByRole("button", { name: "Refresh prices" }));
  expect(await screen.findByText("31 credits", { exact: true })).toBeVisible();
  offline = true;
  fireEvent.click(screen.getByRole("button", { name: "Refresh prices" }));
  expect(
    await screen.findByText(/Offline · Last known prices · flip-b/),
  ).toBeVisible();
  expect(screen.getByText("31 credits", { exact: true })).toBeVisible();
});
