import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { useTurnstile } from "./turnstile";

const siteKey = "1x00000000000000000000AA";

function fixture() {
  const widget = vi
    .fn<
      (
        node: HTMLElement,
        options: {
          action: string;
          callback: (proof: string) => void;
          "expired-callback": () => void;
        },
      ) => string
    >()
    .mockReturnValue("widget");
  const remove = vi.fn();
  vi.stubGlobal("turnstile", { render: widget, remove });
  return { widget, remove };
}

function Probe({
  keyValue,
  submit,
  reload,
}: {
  keyValue: string;
  submit: (proof?: string) => void;
  reload?: () => void;
}) {
  const check = useTurnstile(keyValue, "email_code", true, false, reload);
  return (
    <>
      {check.widget}
      <button
        disabled={!check.ready}
        onClick={() => {
          const proof = check.consume();
          if (proof !== null) submit(proof);
        }}
      >
        Submit
      </button>
      <button onClick={check.fail}>Simulate server rejection</button>
    </>
  );
}

afterEach(() => vi.unstubAllGlobals());

test("a disabled check never renders a widget or adds proof", () => {
  const { widget } = fixture();
  const submit = vi.fn();
  render(<Probe keyValue="" submit={submit} />);
  fireEvent.click(screen.getByRole("button", { name: "Submit" }));
  expect(submit).toHaveBeenCalledWith(undefined);
  expect(widget).not.toHaveBeenCalled();
});

test("a check supplies one proof and focuses retry after rejection", async () => {
  const { widget, remove } = fixture();
  const submit = vi.fn();
  render(<Probe keyValue={siteKey} submit={submit} />);
  const send = screen.getByRole("button", { name: "Submit" });
  expect(send).toBeDisabled();
  await waitFor(() => expect(widget).toHaveBeenCalledOnce());
  expect(widget.mock.calls[0]?.[1].action).toBe("email_code");
  act(() => widget.mock.calls[0]?.[1].callback("first-proof"));
  fireEvent.click(send);
  fireEvent.click(send);
  expect(submit.mock.calls).toEqual([["first-proof"]]);
  fireEvent.click(
    screen.getByRole("button", { name: "Simulate server rejection" }),
  );
  const retry = screen.getByRole("button", {
    name: "Try security check again",
  });
  expect(retry).toHaveFocus();
  expect(screen.getByRole("alert")).toHaveTextContent("Security check failed");
  expect(send).toBeDisabled();
  fireEvent.click(retry);
  await waitFor(() => expect(widget).toHaveBeenCalledTimes(2));
  expect(remove).toHaveBeenCalledWith("widget");
  act(() => widget.mock.calls[1]?.[1].callback("fresh-proof"));
  fireEvent.click(send);
  expect(submit.mock.calls).toEqual([["first-proof"], ["fresh-proof"]]);
});

test("expiry clears outstanding proof before retry", async () => {
  const { widget } = fixture();
  const submit = vi.fn();
  render(<Probe keyValue={siteKey} submit={submit} />);
  await waitFor(() => expect(widget).toHaveBeenCalledOnce());
  const callbacks = widget.mock.calls[0]?.[1];
  act(() => callbacks?.callback("expires"));
  act(() => callbacks?.["expired-callback"]());
  fireEvent.click(screen.getByRole("button", { name: "Submit" }));
  expect(submit).not.toHaveBeenCalled();
  expect(
    screen.getByRole("button", { name: "Try security check again" }),
  ).toHaveFocus();
});

test("an explicit server rejection can recover after configuration was unavailable", () => {
  const { widget } = fixture();
  const reload = vi.fn();
  render(<Probe keyValue="" submit={vi.fn()} reload={reload} />);
  fireEvent.click(
    screen.getByRole("button", { name: "Simulate server rejection" }),
  );
  const retry = screen.getByRole("button", {
    name: "Try security check again",
  });
  expect(retry).toHaveFocus();
  fireEvent.click(retry);
  expect(reload).toHaveBeenCalledOnce();
  expect(widget).not.toHaveBeenCalled();
});
