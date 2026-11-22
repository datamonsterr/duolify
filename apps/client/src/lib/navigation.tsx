import type { AnchorHTMLAttributes, MouseEvent, Ref } from "react";

// In-app navigation without a page load. Web credentials live only in memory, so a full
// load (plain <a href> or location.assign) signs the learner out. The app router registers
// itself here; without one (unit tests, before boot) navigation falls back to a page load.
type Navigator = (to: string) => void;
let navigator: Navigator | null = null;

export function registerNavigator(next: Navigator | null): void {
  navigator = next;
}

// Hover, focus and touch warm the target route's chunk so the click does not wait for it.
type Preloader = (to: string) => void;
let preloader: Preloader | null = null;

export function registerPreloader(next: Preloader | null): void {
  preloader = next;
}

export function navigateTo(to: string): void {
  if (navigator) navigator(to);
  else window.location.assign(to);
}

/** An <a> for same-app paths: keeps its href, navigates in-app on a plain left click. */
export function AppLink({
  href,
  onClick,
  ...props
}: AnchorHTMLAttributes<HTMLAnchorElement> & {
  href: string;
  ref?: Ref<HTMLAnchorElement>;
}) {
  function click(event: MouseEvent<HTMLAnchorElement>) {
    onClick?.(event);
    if (
      event.defaultPrevented ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey ||
      props.target ||
      !navigator
    )
      return;
    event.preventDefault();
    navigateTo(href);
  }
  const warm = () => preloader?.(href);
  return (
    <a
      href={href}
      onClick={click}
      onPointerEnter={warm}
      onFocus={warm}
      onTouchStart={warm}
      {...props}
    />
  );
}
