import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { CreateCharacter } from "./CreateCharacter";
import { characterCreation } from "./creation";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
it("fails closed when approval is missing and offers free defaults", async () => {
  vi.spyOn(characterCreation, "availability").mockResolvedValue({
    enabled: false,
    reason_code: "creation_disabled",
    proposal: false,
  });
  render(<CreateCharacter onCreated={vi.fn()} />);
  expect(
    await screen.findByText(
      /Creation is unavailable until pricing is approved/,
    ),
  ).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Review character quote" }),
  ).toBeDisabled();
  expect(screen.getByRole("link", { name: "Use Default cast" })).toBeTruthy();
});
it("requires a versioned quote confirmation and reuses a job on retry", async () => {
  vi.spyOn(characterCreation, "availability").mockResolvedValue({
    enabled: true,
    proposal: true,
    reason_code: "",
  });
  const job = {
    id: "job-1",
    state: "quoted",
    quote: {
      id: "q-1",
      pricing_version: "unapproved-test",
      content_ceiling: 7,
    },
    proposal: true,
  };
  const quote = vi.spyOn(characterCreation, "quote").mockResolvedValue(job);
  const confirm = vi
    .spyOn(characterCreation, "confirm")
    .mockRejectedValue(new Error("Connect and retry."));
  render(<CreateCharacter onCreated={vi.fn()} />);
  await waitFor(() =>
    expect(
      screen.getByLabelText("Describe your original character"),
    ).toBeEnabled(),
  );
  fireEvent.change(screen.getByLabelText("Describe your original character"), {
    target: { value: "Original gardener" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Review character quote" }),
  );
  expect(
    await screen.findByText(/Pricing version: unapproved-test/),
  ).toBeTruthy();
  expect(confirm).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: "Confirm character credits" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Connect and retry.",
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Confirm character credits" }),
  );
  await waitFor(() => expect(confirm).toHaveBeenCalledTimes(2));
  expect(quote).toHaveBeenCalledTimes(1);
  expect(confirm).toHaveBeenLastCalledWith("job-1", "q-1");
});
