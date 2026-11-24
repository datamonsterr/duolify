import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Button } from "./Button";
import { Icon } from "./Icon";
import {
  actionLevelViolations,
  countActionLevels,
  gapViolations,
  iconOnlyViolations,
  reachViolations,
} from "./humanFactors";

describe("action levels", () => {
  it("counts one primary and up to two secondary actions", () => {
    const { container } = render(
      <div>
        <Button data-action-level="primary">Save</Button>
        <Button variant="secondary" data-action-level="secondary">
          Skip
        </Button>
        <Button variant="quiet">More</Button>
      </div>,
    );
    expect(countActionLevels(container)).toEqual({ primary: 1, secondary: 1 });
    expect(actionLevelViolations(container)).toEqual([]);
  });
  it("fails two primaries and three secondaries", () => {
    const { container } = render(
      <div>
        <Button data-action-level="primary">One</Button>
        <Button data-action-level="primary">Two</Button>
        <Button data-action-level="secondary">A</Button>
        <Button data-action-level="secondary">B</Button>
        <Button data-action-level="secondary">C</Button>
      </div>,
    );
    expect(actionLevelViolations(container)).toHaveLength(2);
  });
  it("ignores controls inside hidden sections", () => {
    const { container } = render(
      <div>
        <Button data-action-level="primary">One</Button>
        <section hidden>
          <Button data-action-level="primary">Two</Button>
        </section>
      </div>,
    );
    expect(actionLevelViolations(container)).toEqual([]);
  });
});

describe("icon-only allowlist", () => {
  it("allows the universal set with an accessible name", () => {
    const { container } = render(
      <button aria-label="Close">
        <Icon name="close" />
      </button>,
    );
    expect(iconOnlyViolations(container)).toEqual([]);
  });
  it("fails an icon outside the set and a missing name", () => {
    const { container } = render(
      <div>
        <button aria-label="Goal">
          <Icon name="goal" />
        </button>
        <button>
          <Icon name="close" />
        </button>
      </div>,
    );
    expect(iconOnlyViolations(container)).toEqual([
      'icon-only "goal" is not in the universal set',
      'icon-only "close" has no accessible name',
    ]);
  });
  it("accepts an icon with a visible label", () => {
    const { container } = render(
      <button>
        <Icon name="goal" />
        Goals
      </button>,
    );
    expect(iconOnlyViolations(container)).toEqual([]);
  });
});

describe("geometry rules", () => {
  const viewport = { width: 390, height: 844 };
  it("accepts a primary action in the lower 40% above the inset", () => {
    expect(
      reachViolations({ x: 20, y: 700, width: 350, height: 52 }, viewport, 34),
    ).toEqual([]);
  });
  it("flags top placement, small size and inset overlap", () => {
    expect(
      reachViolations({ x: 20, y: 100, width: 40, height: 40 }, viewport),
    ).toHaveLength(2);
    expect(
      reachViolations({ x: 20, y: 800, width: 350, height: 44 }, viewport, 34),
    ).toHaveLength(1);
  });
  it("requires 8px between neighbours", () => {
    const a = { x: 0, y: 0, width: 44, height: 44 };
    expect(gapViolations([a, { ...a, x: 52 }])).toEqual([]);
    expect(gapViolations([a, { ...a, x: 48 }])).toHaveLength(1);
  });
});

describe("colour is never the only signal", () => {
  it("gives every feedback tone an icon and a text title", async () => {
    const { Feedback } = await import("./chat/Feedback");
    const tones = ["info", "warning", "danger", "success"] as const;
    const icons = new Set<string | null>();
    for (const tone of tones) {
      const { container, unmount } = render(
        <Feedback tone={tone} title="State" />,
      );
      const icon = container.querySelector("[data-icon]");
      expect(icon, tone).not.toBeNull();
      expect(container.textContent).toContain("State");
      icons.add(icon!.getAttribute("data-icon"));
      unmount();
    }
    // Error and success are told apart by shape, not by hue.
    expect(icons.size).toBe(4);
  });
});
