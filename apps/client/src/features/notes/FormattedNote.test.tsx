import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FormattedNote } from "./FormattedNote";

describe("FormattedNote", () => {
  it("shows the title and body with a Show original switch", () => {
    const onToggle = vi.fn();
    render(
      <FormattedNote
        note={{ id: "n", title: "Channels", formatted_body: "- Blocks." }}
        showOriginal={false}
        onToggle={onToggle}
      />,
    );
    expect(screen.getByText("Channels")).toBeTruthy();
    expect(screen.getByText("- Blocks.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show original" }));
    expect(onToggle).toHaveBeenCalled();
  });
  it("offers Hide original while the original shows, and renders nothing for a raw note", () => {
    const { rerender, container } = render(
      <FormattedNote
        note={{ id: "n", title: "T", formatted_body: "B" }}
        showOriginal
        onToggle={() => undefined}
      />,
    );
    expect(screen.getByRole("button", { name: "Hide original" })).toBeTruthy();
    rerender(
      <FormattedNote
        note={{ id: "n" }}
        showOriginal={false}
        onToggle={() => undefined}
      />,
    );
    expect(container.innerHTML).toBe("");
  });
});
