import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useUiPreferences } from "../design/localization/Preferences";
import { Button } from "../design/Button";
import { checkoutAPI, type CheckoutOrder } from "./checkout";

export function CheckoutReturn({
  id,
  onReturn,
}: {
  id: string;
  onReturn: (order?: CheckoutOrder) => void;
}) {
  const [cancelled, setCancelled] = useState(false);
  const { number, currency } = useUiPreferences();
  const query = useQuery({
    queryKey: ["checkout", id],
    queryFn: () => checkoutAPI.get(id),
    retry: false,
  });
  const order = query.data;
  return (
    <section className="pricing-card quote-panel" aria-label="Checkout return">
      <h2>Illustrative checkout — no payment is taken</h2>
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
          <p role="status">
            {cancelled
              ? "No credits were added. Your draft is ready."
              : "Payment pending. Your balance has not changed."}
          </p>
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
          <p>
            Illustrative orders cannot become paid. Live billing verification
            will be available separately.
          </p>
          <Button variant="secondary" onClick={() => setCancelled(true)}>
            Cancel preview
          </Button>
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
