import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { GuidedScriptChat } from "./GuidedScriptChat";
import { accountDelete } from "./account.scripts";

afterEach(() => vi.useRealTimers());
it("submits once per confirmation or retry even when every attempt fails", async () => {
  vi.useFakeTimers();
  const submit = vi.fn().mockRejectedValue(new Error("offline"));
  render(<GuidedScriptChat script={accountDelete} onSubmit={submit} />);
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Delete forever" }));
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(submit).toHaveBeenCalledTimes(1);
  for (let count = 2; count <= 3; count++) {
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(submit).toHaveBeenCalledTimes(count);
  }
});
