import { Suspense, lazy, useCallback, useState, type Ref } from "react";
import type { useTurnstile } from "./turnstile";

type Action = Parameters<typeof useTurnstile>[1];
type Check = Omit<ReturnType<typeof useTurnstile>, "widget"> & {
  siteKey: string | null;
  interactionOnly: boolean;
};

export type TurnstileWidgetProps = {
  ref: Ref<Check>;
  siteKey: string | null;
  action: Action;
  interactionOnly: boolean;
  reloadConfig?: () => void;
  initiallyFailed: boolean;
};

const TurnstileWidget = lazy(() => import("./turnstile"));
const noProof = () => undefined;
const proofPending = () => null;
const noReset = () => {};

export function useTurnstileCheck(
  siteKey: string | null,
  action: Action,
  active = true,
  interactionOnly = false,
  reloadConfig?: () => void,
) {
  const [widgetCheck, setWidgetCheck] = useState<Check | null>(null);
  const [rejected, setRejected] = useState(false);
  // A server rejection confirms a check is required even if config was unavailable.
  const markRejected = useCallback(() => setRejected(true), []);
  const retryConfig = useCallback(() => {
    setRejected(false);
    reloadConfig?.();
  }, [reloadConfig]);
  const showing = active && (Boolean(siteKey) || rejected);
  const check =
    showing &&
    widgetCheck?.siteKey === siteKey &&
    widgetCheck.interactionOnly === interactionOnly
      ? widgetCheck
      : null;
  const readyWithoutProof = !active || (siteKey === "" && !rejected);

  return {
    ready: readyWithoutProof || Boolean(check?.ready),
    consume: readyWithoutProof ? noProof : (check?.consume ?? proofPending),
    fail: check?.fail ?? markRejected,
    reset: check?.reset ?? noReset,
    widget: showing ? (
      <Suspense fallback={null}>
        <TurnstileWidget
          ref={setWidgetCheck}
          siteKey={siteKey}
          action={action}
          interactionOnly={interactionOnly}
          reloadConfig={retryConfig}
          initiallyFailed={rejected}
        />
      </Suspense>
    ) : null,
  };
}
