import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useUiPreferences } from "../design/localization/Preferences";
import { Button } from "../design/Button";
import { checkoutAPI, type CheckoutOrder } from "./checkout";

export function CheckoutReturn({
  id,
  onReturn,
  onChooseOption,
}: {
  id: string;
  onReturn: (order?: CheckoutOrder) => void;
  onChooseOption?: () => void;
}) {
  const [cancelled, setCancelled] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  const client = useQueryClient();
  const { number, currency } = useUiPreferences();
  const query = useQuery({
    queryKey: ["checkout", id],
    queryFn: () => checkoutAPI.get(id),
    retry: false,
  });
  const order = query.data;
  const paid = order?.state === "paid";
  const failed = order?.state === "failed" || order?.state === "expired";
  useEffect(() => {
    heading.current?.focus();
    if (paid) void client.invalidateQueries({ queryKey: ["wallet"] });
  }, [paid, client]);
  return (
    <section
      className="pricing-card quote-panel checkout-return"
      data-state={order?.state}
      aria-label="Checkout return"
    >
      {paid && (
        <div className="checkout-verified-mark" aria-hidden="true">
          ✓
        </div>
      )}
      <h2 ref={heading} tabIndex={-1}>
        {paid
          ? order?.billing_period !== "one_time"
            ? "Plan activated"
            : "Credits added"
          : order?.illustrative === false
            ? "Test checkout — no real payment is taken"
            : "Illustrative checkout — no payment is taken"}
      </h2>
      <p>Your saved lessons and notes stay available.</p>
      {query.isPending && <p role="status">Checking server order status…</p>}
      {query.isError && (
        <p role="alert">
          Payment status unavailable. Check your connection and retry; your work
          is saved.
        </p>
      )}
      {order && (
        <>
          <p role="status" className="checkout-status">
            {paid
              ? order?.billing_period !== "one_time"
                ? "Paid invoice verified. Your plan and monthly credits are ready in your wallet."
                : "Payment verified. Purchased credits are ready in your wallet."
              : failed
                ? "No credits were added. Your draft is ready."
                : cancelled
                  ? "No credits were added. Your draft is ready."
                  : "Payment pending. Your balance has not changed."}
          </p>
          {failed && (
            <p role="alert" className="checkout-failure">
              {order.state === "expired"
                ? "Checkout expired"
                : "Payment didn't complete"}
              . No credits were added. Choose another option or check payment
              status.
            </p>
          )}
          <p>
            {number(order.credits)} credits
            {order.billing_period !== "one_time" &&
              " each service month"} ·{" "}
            {currency(order.amount_minor / 100, order.currency)}{" "}
            {order.billing_period === "yearly"
              ? "/ year"
              : order.billing_period === "monthly"
                ? "/ month"
                : "one-time"}{" "}
            + applicable tax
          </p>
          {order.illustrative && (
            <p>
              Illustrative orders cannot become paid. Live billing verification
              will be available separately.
            </p>
          )}
          {!paid && !failed && order.illustrative && (
            <Button variant="secondary" onClick={() => setCancelled(true)}>
              Cancel preview
            </Button>
          )}
          {failed && (
            <Button onClick={onChooseOption ?? (() => onReturn(order))}>
              Try again
            </Button>
          )}
          {failed && (
            <Button
              variant="secondary"
              onClick={onChooseOption ?? (() => onReturn(order))}
            >
              Choose another option
            </Button>
          )}
        </>
      )}
      <Button disabled={query.isFetching} onClick={() => void query.refetch()}>
        Check payment status
      </Button>
      <Button variant="quiet" onClick={() => onReturn(order)}>
        Return to saved work
      </Button>
    </section>
  );
}
