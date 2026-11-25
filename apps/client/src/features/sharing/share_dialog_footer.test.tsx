import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ShareCoursePanel } from "./ShareCourse";
import { session } from "../accounts/session";
import { drafts } from "./drafts";
import { actionFooterViolations } from "../design/humanFactors";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));

beforeEach(() => {
  localStorage.clear();
  vi.mocked(session.request).mockReset();
  vi.spyOn(drafts, "disclosure").mockResolvedValue({
    names: [],
    character_count: 0,
    custom_count: 0,
    exports_enabled: false,
  });
});

const footerOf = (dialog: HTMLElement) =>
  dialog.querySelector<HTMLElement>(":scope > [data-action-footer]")!;

describe("share course dialog footer", () => {
  it("keeps one footer as the dialog's last element, Cancel in it, none mid-dialog", async () => {
    vi.mocked(session.request).mockResolvedValue({
      visibility: "private",
      active: false,
    });
    const onClose = vi.fn();
    render(<ShareCoursePanel courseId="c1" onClose={onClose} />);
    const dialog = screen.getByTestId("share-course-panel");
    // Editing an answer used to render the chat's own Cancel in the middle of the dialog.
    fireEvent.click(
      await screen.findByRole("button", { name: "Change answer: Only me" }),
    );
    await screen.findAllByRole("radio");

    const footer = footerOf(dialog);
    expect(dialog.lastElementChild).toBe(footer);
    const cancels = within(dialog).getAllByRole("button", { name: "Cancel" });
    expect(cancels).toHaveLength(1);
    expect(footer.contains(cancels[0])).toBe(true);
    expect(actionFooterViolations(dialog)).toEqual([]);

    fireEvent.click(cancels[0]);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("puts the chat's Apply in the footer, right of Cancel, and it still submits", async () => {
    vi.mocked(session.request).mockResolvedValue({
      visibility: "private",
      active: false,
    });
    render(<ShareCoursePanel courseId="c1" onClose={() => {}} />);
    const dialog = screen.getByTestId("share-course-panel");
    const footer = footerOf(dialog);
    const apply = await within(footer).findByRole("button", { name: "Apply" });
    expect(
      within(footer)
        .getAllByRole("button")
        .map((b) => b.textContent),
    ).toEqual(["Cancel", "Apply"]);
    expect(actionFooterViolations(dialog)).toEqual([]);
    fireEvent.click(apply);
    expect(await screen.findByText(/Sharing updated/)).toBeTruthy();
  });
});
