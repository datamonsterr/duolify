import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { CheckoutReturn } from "./CheckoutReturn";
import { checkoutAPI, type CheckoutOrder } from "./checkout";
import approved from "../pricing/fixtures/approved.json";

afterEach(() => vi.restoreAllMocks());
function show(state: string) {
  const pack = approved.packs[0];
  const order = {
    id: "fixture-order",
    state,
    illustrative: false,
    sku: pack.sku,
    pricing_version: approved.version,
    amount_minor: pack.amount_minor,
    currency: pack.currency,
    credits: pack.credits,
    billing_period: "one_time",
    return_to: "/credits",
    quote_id: "",
  } as CheckoutOrder;
  vi.spyOn(checkoutAPI, "get").mockResolvedValue(order);
  const back = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <CheckoutReturn id={order.id} onReturn={back} />
    </QueryClientProvider>,
  );
  return { back, order };
}
it("shows verified success only from server state and returns without executing an action", async () => {
  const { back, order } = show("paid");
  expect(
    await screen.findByRole("heading", { name: /^Credits added$/ }),
  ).toBeVisible();
  expect(screen.getByText(/Payment verified/)).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Cancel preview" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Return to saved work" }));
  expect(back).toHaveBeenCalledWith(order);
});
it.each(["failed", "expired"])(
  "shows %s recovery without granting success",
  async (state) => {
    show(state);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      state === "failed" ? "Payment didn't complete" : "Checkout expired",
    );
    expect(
      screen.queryByRole("heading", { name: "Credits added" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Check payment status" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Choose another option" }),
    ).toBeEnabled();
  },
);
it("keeps unpaid test checkout pending even when browser claims success", async () => {
  history.replaceState(null, "", "/?payment=success");
  show("pending");
  expect(await screen.findByText(/Payment pending/)).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: "Credits added" }),
  ).not.toBeInTheDocument();
});
