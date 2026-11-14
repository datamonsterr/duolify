import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { ReplanPanel } from "./ReplanPanel";

it("requires explicit consent, displays server price and retries the same quote after transport loss", async () => {
  const service = {
    quote: vi.fn().mockResolvedValue({
      id: "q",
      content_ceiling: 73,
      pricing_version: "v2",
      expires_at: "2099-01-01T00:00:00Z",
    }),
    accept: vi.fn().mockResolvedValue(undefined),
    propose: vi
      .fn()
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValue({
        id: "p",
        state: "proposed",
        outline_version: 1,
        expires_at: "2099-01-02T00:00:00Z",
        diff: [
          {
            kind: "removed",
            lesson_id: "l2",
            unit_title: "Unit",
            before: "History",
          },
        ],
      }),
  };
  render(<ReplanPanel courseId="c" service={service} onBack={() => {}} />);
  expect(await screen.findByText("Up to 73 credits")).toBeVisible();
  expect(service.accept).not.toHaveBeenCalled();
  expect(service.propose).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: "Accept quote and prepare proposal" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Couldn’t prepare a proposal",
  );
  fireEvent.click(screen.getByRole("button", { name: "Check proposal again" }));
  expect(
    await screen.findByRole("heading", { name: "Proposed changes" }),
  ).toBeVisible();
  expect(service.quote).toHaveBeenCalledTimes(1);
  expect(service.propose.mock.calls).toEqual([
    ["c", "q"],
    ["c", "q"],
  ]);
  expect(
    screen.queryByRole("button", { name: /Apply/ }),
  ).not.toBeInTheDocument();
});
