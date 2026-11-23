import { describe, expect, it, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { CharacterCreator } from "./CharacterCreator";
import { characterLibrary } from "./library";

vi.mock("./library", () => ({
  characterLibrary: { createV2: vi.fn(), editV2: vi.fn() },
}));

const tab = (name: string) => screen.getByRole("tab", { name });
beforeEach(() => vi.clearAllMocks());

describe("CharacterCreator", () => {
  it("shows four tabs and live thumbnails as radio tiles", () => {
    render(<CharacterCreator onSaved={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getAllByRole("tab")).toHaveLength(4);
    const kinds = screen.getByRole("group", { name: "Kind" });
    expect(within(kinds).getAllByRole("radio")).toHaveLength(4);
    expect(kinds.querySelectorAll("svg").length).toBe(4);
    expect(screen.getByRole("img")).toHaveAccessibleName();
  });

  it("kind switch resets the body and species switch resets species attributes", () => {
    render(<CharacterCreator onSaved={vi.fn()} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("radio", { name: "Animal" }));
    const species = screen.getByRole("group", { name: "Species" });
    expect(within(species).getByRole("radio", { name: "Fox" })).toBeChecked();
    fireEvent.click(within(species).getByRole("radio", { name: "Cat" }));
    fireEvent.click(tab("Look"));
    const tail = screen.getByRole("group", { name: "Tail" });
    fireEvent.click(within(tail).getByRole("radio", { name: "Fluffy" }));
    expect(within(tail).getByRole("radio", { name: "Fluffy" })).toBeChecked();
    fireEvent.click(tab("Kind"));
    fireEvent.click(screen.getByRole("radio", { name: "Dog" }));
    fireEvent.click(tab("Look"));
    // Dog resets to its own first allowed tail, not the earlier choice.
    const dogTail = screen.getByRole("group", { name: "Tail" });
    expect(within(dogTail).getByRole("radio", { name: "Short" })).toBeChecked();
  });

  it("species-limited lists only offer allowed ears", () => {
    render(<CharacterCreator onSaved={vi.fn()} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("radio", { name: "Animal" }));
    fireEvent.click(tab("Look"));
    const ears = screen.getByRole("group", { name: "Ears" });
    expect(within(ears).getAllByRole("radio").length).toBeLessThanOrEqual(3);
  });

  it("moves between tabs with arrow keys", () => {
    render(<CharacterCreator onSaved={vi.fn()} onClose={vi.fn()} />);
    tab("Kind").focus();
    fireEvent.keyDown(tab("Kind"), { key: "ArrowRight" });
    expect(tab("Look")).toHaveAttribute("aria-selected", "true");
  });

  it("creates through the API with the draft document", async () => {
    vi.mocked(characterLibrary.createV2).mockResolvedValue({
      id: "c1",
      version: 1,
    } as never);
    const onSaved = vi.fn();
    render(<CharacterCreator onSaved={onSaved} onClose={vi.fn()} />);
    fireEvent.click(tab("About"));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Mira" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    const sent = vi.mocked(characterLibrary.createV2).mock.calls[0]![0];
    expect(sent.info.name).toBe("Mira");
    expect(sent.schema_version).toBe(2);
  });
});
