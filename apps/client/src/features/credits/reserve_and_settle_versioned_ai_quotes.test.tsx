import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { it, expect, vi } from "vitest";
import { ReservationPanel } from "./ReservationPanel";
import { reservationAPI } from "./reservations";

it("requires explicit reservation, shows next grant on insufficiency and permits retry", async () => {
  const reserve = vi
    .spyOn(reservationAPI, "reserve")
    .mockRejectedValueOnce(
      new Error("insufficient_credits. Next daily grant: tomorrow"),
    );
  render(<ReservationPanel quoteId="quote" />);
  expect(reserve).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Reserve credits" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Next daily grant",
  );
  reserve.mockResolvedValue({
    id: "job",
    pricing_version: "original",
    state: "reserved",
    content_spent: 0,
    audio_spent: 0,
  });
  fireEvent.click(screen.getByRole("button", { name: "Reserve credits" }));
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent("Credits reserved"),
  );
  vi.spyOn(reservationAPI, "release").mockResolvedValue({
    id: "job",
    pricing_version: "original",
    state: "released",
    content_spent: 0,
    audio_spent: 0,
  });
  fireEvent.click(screen.getByRole("button", { name: "Release reservation" }));
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent("Credits released"),
  );
});
