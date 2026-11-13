import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { CourseCreate } from "./CourseCreate";

it("requires final quote consent and keeps edits after failure", async () => {
  const snapshot = {
    draft_id: "draft",
    revision: 7,
    content_hash: "hash",
    outline: [
      {
        id: "u",
        title: "Manual",
        lessons: [{ id: "l", title: "Added", goal: "Learn" }],
      },
    ],
  };
  const service = {
    quote: vi.fn().mockResolvedValue({
      id: "quote",
      content_ceiling: 55,
      audio_ceiling: 0,
      pricing_version: "fixture",
      expires_at: new Date(Date.now() + 60000).toISOString(),
    }),
    wallet: vi.fn().mockResolvedValue({ purchased: 100, subscription: 0 }),
    accept: vi.fn().mockResolvedValue({}),
    create: vi.fn().mockRejectedValue(new Error("Offline")),
    get: vi.fn(),
    watch: vi.fn(),
  };
  render(
    <CourseCreate snapshot={snapshot} onBack={() => {}} service={service} />,
  );
  expect(service.create).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Review creation cost" }));
  await screen.findByText(/Reserve up to 55 credits/);
  expect(service.create).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: /Create outline.*55/ }));
  await waitFor(() =>
    expect(service.create).toHaveBeenCalledWith(snapshot, "quote"),
  );
  expect(service.accept).toHaveBeenCalledWith("quote");
  expect(await screen.findByRole("alert")).toHaveTextContent(/edits are kept/);
});
