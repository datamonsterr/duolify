import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AppLink, navigateTo, registerNavigator } from "./navigation";

afterEach(() => registerNavigator(null));

describe("in-app navigation", () => {
  it("navigateTo uses the registered router instead of a page load", () => {
    const navigate = vi.fn();
    registerNavigator(navigate);
    const before = window.location.href;
    navigateTo("/courses/create");
    expect(navigate).toHaveBeenCalledWith("/courses/create");
    expect(window.location.href).toBe(before);
  });

  it("AppLink keeps its href and navigates in-app on a plain click", () => {
    const navigate = vi.fn();
    registerNavigator(navigate);
    render(<AppLink href="/courses/create?topic=x">Create a course</AppLink>);
    const link = screen.getByRole("link", { name: "Create a course" });
    expect(link).toHaveAttribute("href", "/courses/create?topic=x");
    const notPrevented = fireEvent.click(link);
    expect(notPrevented).toBe(false);
    expect(navigate).toHaveBeenCalledWith("/courses/create?topic=x");
  });

  it("AppLink leaves modified clicks to the browser", () => {
    const navigate = vi.fn();
    registerNavigator(navigate);
    render(<AppLink href="/credits">Credits</AppLink>);
    fireEvent.click(screen.getByRole("link"), { ctrlKey: true });
    expect(navigate).not.toHaveBeenCalled();
  });
});
