import { lazy, Suspense } from "react";
const Onboarding = lazy(() => import("./Onboarding"));
// No welcome, requests or heavy tour code on invite/lesson/SSO destinations.
export function OnboardingGate({ path }: { path: string }) {
  return path === "/" ? (
    <Suspense fallback={null}>
      <Onboarding />
    </Suspense>
  ) : null;
}
