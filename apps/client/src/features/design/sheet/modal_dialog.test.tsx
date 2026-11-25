import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { ModalDialog } from "./ModalDialog";

function Host() {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button onClick={() => setOpen(true)}>Open</button>
      {open && (
        <ModalDialog onClose={() => setOpen(false)} aria-label="Confirm">
          <button>First</button>
          <button>Last</button>
        </ModalDialog>
      )}
    </div>
  );
}

describe("ModalDialog", () => {
  it("focuses inside, wraps Tab at both ends, closes on Escape, restores focus", () => {
    render(<Host />);
    const opener = screen.getByRole("button", { name: "Open" });
    opener.focus();
    fireEvent.click(opener);
    const first = screen.getByText("First");
    const last = screen.getByText("Last");
    expect(document.activeElement).toBe(first);
    fireEvent.keyDown(first, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(last);
    fireEvent.keyDown(last, { key: "Tab" });
    expect(document.activeElement).toBe(first);
    fireEvent.keyDown(first, { key: "Escape" });
    expect(screen.queryByText("First")).toBeNull();
    expect(document.activeElement).toBe(opener);
  });
});
