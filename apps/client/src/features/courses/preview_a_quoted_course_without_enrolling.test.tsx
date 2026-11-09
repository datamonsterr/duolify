import { render, screen, fireEvent } from "@testing-library/react";
import { it, expect, vi } from "vitest";
import { CoursePreview } from "./CoursePreview";
import { emptyBrief } from "./brief";
import { http, HttpResponse } from "msw";
import { server } from "../../test/setup";
import { session, credentialStore } from "../accounts/session";

it("requires fresh consent, preserves draft on failure and invalidates a quote on revision change", async () => {
  const service = {
    quote: vi.fn().mockResolvedValue({
      id: "q",
      content_ceiling: 25,
      pricing_version: "v1",
      expires_at: new Date(Date.now() + 300000).toISOString(),
    }),
    accept: vi.fn().mockResolvedValue({ accepted: true }),
    preview: vi
      .fn()
      .mockRejectedValue(new Error("Reserved credits were released.")),
  };
  const revision = {
    id: "r1",
    content_hash: "h",
    brief: { ...emptyBrief(), topic: "Sky" },
  };
  const back = vi.fn();
  const { rerender } = render(
    <CoursePreview revision={revision} service={service} onBack={back} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Review preview cost" }));
  await screen.findByText(/v1/);
  expect(service.preview).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: /Confirm & preview/ }));
  await screen.findByRole("alert");
  expect(service.accept).toHaveBeenCalledWith("q");
  expect(service.preview).toHaveBeenCalledWith("r1", "q");
  rerender(
    <CoursePreview
      revision={{ ...revision, id: "r2" }}
      service={service}
      onBack={back}
    />,
  );
  expect(
    screen.queryByRole("button", { name: /Confirm & preview/ }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Back to brief" }));
  expect(back).toHaveBeenCalled();
});

for (const mode of ["expired", "insufficient"]) {
  it(`does not start work with ${mode} terms`, async () => {
    const service = {
      quote: vi.fn().mockResolvedValue({
        id: "q",
        content_ceiling: 25,
        pricing_version: "v1",
        expires_at: new Date(
          Date.now() + (mode === "expired" ? -10000 : 300000),
        ).toISOString(),
      }),
      wallet: vi.fn().mockResolvedValue({
        purchased: mode === "insufficient" ? 0 : 100,
        subscription: 0,
      }),
      accept: vi.fn(),
      preview: vi.fn(),
    };
    render(
      <CoursePreview
        revision={{
          id: "r",
          content_hash: "h",
          brief: { ...emptyBrief(), topic: "Sky" },
        }}
        service={service}
        onBack={() => {}}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Review preview cost" }),
    );
    await screen.findByRole("alert");
    const confirm = screen.queryByRole("button", { name: /Confirm & preview/ });
    if (mode === "expired") expect(confirm).not.toBeInTheDocument();
    else expect(confirm).toBeDisabled();
    expect(service.preview).not.toHaveBeenCalled();
    expect(service.accept).not.toHaveBeenCalled();
  });
}

it("preserves the accepted quote header through access token refresh", async () => {
  await credentialStore.write({
    access_token: "old",
    refresh_token: "fixture-refresh",
    expires_in: 900,
  });
  const seen: string[] = [];
  server.use(
    http.post("*/v1/courses/preview", async ({ request }) => {
      seen.push(request.headers.get("X-Quote-ID") ?? "");
      expect(await request.json()).toEqual({ brief_revision: "revision" });
      return request.headers.get("Authorization") === "Bearer fresh"
        ? HttpResponse.json({ ok: true })
        : HttpResponse.json({}, { status: 401 });
    }),
    http.post("*/v1/auth/refresh", () =>
      HttpResponse.json({
        access_token: "fresh",
        refresh_token: "rotated",
        expires_in: 900,
      }),
    ),
  );
  try {
    await session.request(
      "/v1/courses/preview",
      "POST",
      { brief_revision: "revision" },
      { "X-Quote-ID": "accepted-quote" },
    );
    expect(seen).toEqual(["accepted-quote", "accepted-quote"]);
  } finally {
    session.clear();
    await credentialStore.write(null);
  }
});
