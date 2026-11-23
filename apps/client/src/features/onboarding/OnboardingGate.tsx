import { Component, lazy, Suspense, type ReactNode } from "react";
const Onboarding = lazy(() => import("./Onboarding"));
// Onboarding is optional: if its chunk or CSS cannot load (offline on first visit to Learn), render
// nothing instead of letting the error replace the whole page.
class OptionalBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? null : this.props.children;
  }
}
// No welcome, requests or heavy tour code on invite/lesson/SSO destinations.
export function OnboardingGate({ path }: { path: string }) {
  return path === "/" ? (
    <OptionalBoundary>
      <Suspense fallback={null}>
        <Onboarding />
      </Suspense>
    </OptionalBoundary>
  ) : null;
}
