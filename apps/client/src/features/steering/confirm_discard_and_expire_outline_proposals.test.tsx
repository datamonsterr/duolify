import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { ReplanPanel } from "./ReplanPanel";
import { AuthError } from "../accounts/session";

const quote = {
  id: "q",
  content_ceiling: 55,
  pricing_version: "v2",
  expires_at: "2099-01-01T00:00:00Z",
};

const proposal = {
  id: "p1",
  state: "proposed",
  outline_version: 1,
  expires_at: "2099-01-02T00:00:00Z",
  diff: [
    {
      kind: "retitled",
      lesson_id: "l2",
      unit_title: "Unit 3",
      before: "History",
      after: "Go examples",
    },
  ],
};

it("opens confirm dialog when Apply changes is clicked and confirms proposal", async () => {
  const service = {
    quote: vi.fn().mockResolvedValue(quote),
    accept: vi.fn().mockResolvedValue(undefined),
    propose: vi.fn().mockResolvedValue(proposal),
    confirm: vi.fn().mockResolvedValue({
      id: "p1",
      state: "proposed",
      decision: "confirmed",
      outline_version: 2,
    }),
    discard: vi.fn().mockResolvedValue({
      id: "p1",
      state: "proposed",
      decision: "discarded",
    }),
  };

  render(
    <ReplanPanel
      courseId="c"
      service={service}
      onBack={() => {}}
      initial={{ quote, proposal, accepted: true }}
    />,
  );

  const applyBtn = await screen.findByRole("button", { name: "Apply changes" });
  expect(applyBtn).toBeVisible();
  const discardBtn = screen.getByRole("button", { name: "Discard" });
  expect(discardBtn).toBeVisible();

  // Click Apply changes opens confirm dialog
  fireEvent.click(applyBtn);
  const dialog = screen.getByRole("dialog", { name: /apply|confirm/i });
  expect(dialog).toBeVisible();
  expect(dialog).toHaveTextContent(/generated and unlocked lessons/i);

  // Cancel dialog
  const cancelBtn = screen.getByRole("button", { name: "Cancel" });
  fireEvent.click(cancelBtn);
  expect(
    screen.queryByRole("dialog", { name: /apply|confirm/i }),
  ).not.toBeInTheDocument();
  expect(service.confirm).not.toHaveBeenCalled();

  // Re-open dialog and confirm
  fireEvent.click(applyBtn);
  const confirmBtn = screen
    .getByRole("dialog")
    .querySelector("button:not([aria-label='Cancel']):last-child");
  expect(confirmBtn).toBeTruthy();
  fireEvent.click(confirmBtn!);

  expect(service.confirm).toHaveBeenCalledWith("c", "p1");
  expect(
    await screen.findByRole("heading", { name: "Outline updated" }),
  ).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent(
    "Your outline has been updated with the confirmed changes.",
  );
});

it("displays stale banner when confirm fails with version_conflict", async () => {
  const service = {
    quote: vi.fn().mockResolvedValue(quote),
    accept: vi.fn().mockResolvedValue(undefined),
    propose: vi.fn().mockResolvedValue(proposal),
    confirm: vi.fn().mockRejectedValue(
      new AuthError("Stale proposal", 409, {
        reason_code: "version_conflict",
      }),
    ),
    discard: vi.fn(),
  };

  render(
    <ReplanPanel
      courseId="c"
      service={service}
      onBack={() => {}}
      initial={{ quote, proposal, accepted: true }}
    />,
  );

  const applyBtn = await screen.findByRole("button", { name: "Apply changes" });
  fireEvent.click(applyBtn);
  const confirmBtn = screen
    .getByRole("dialog")
    .querySelector("button:not([aria-label='Cancel']):last-child");
  fireEvent.click(confirmBtn!);

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "This proposal is out of date",
  );
  expect(screen.getByRole("alert")).toHaveTextContent(
    "Your outline changed since it was made",
  );
  expect(
    screen.getByRole("button", {
      name: /request a new proposal|review a new quote/i,
    }),
  ).toBeVisible();
});

it("discards proposal and keeps settled charge without applying changes", async () => {
  const onBack = vi.fn();
  const service = {
    quote: vi.fn().mockResolvedValue(quote),
    accept: vi.fn().mockResolvedValue(undefined),
    propose: vi.fn().mockResolvedValue(proposal),
    confirm: vi.fn(),
    discard: vi.fn().mockResolvedValue({
      id: "p1",
      state: "proposed",
      decision: "discarded",
    }),
  };

  render(
    <ReplanPanel
      courseId="c"
      service={service}
      onBack={onBack}
      initial={{ quote, proposal, accepted: true }}
    />,
  );

  const discardBtn = await screen.findByRole("button", { name: "Discard" });
  fireEvent.click(discardBtn);

  expect(service.discard).toHaveBeenCalledWith("c", "p1");
  expect(service.confirm).not.toHaveBeenCalled();
  expect(await screen.findByText("Proposal discarded")).toBeVisible();
});
