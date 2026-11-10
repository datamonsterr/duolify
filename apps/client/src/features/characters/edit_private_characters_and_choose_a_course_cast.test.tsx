import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { CharacterLibrary } from "./CharacterLibrary";
import { CourseCast } from "./CourseCast";
import { characterLibrary } from "./library";
import { validateCharacterSpec } from "./schema";
import { defaultGuideFixture } from "./fixtures";

vi.mock("./library", () => ({
  characterLibrary: {
    list: vi.fn(),
    edit: vi.fn(),
    delete: vi.fn(),
    cast: vi.fn(),
    select: vi.fn(),
  },
}));

describe("free structured Character validation", () => {
  it("requires every field with exact JSON types and measures Unicode code points", () => {
    const original = structuredClone(defaultGuideFixture);
    const missing = structuredClone(original) as unknown as {
      info: Record<string, unknown>;
    };
    delete missing.info.bio;
    expect(validateCharacterSpec(missing).valid).toBe(false);
    expect(
      validateCharacterSpec({
        ...original,
        info: { ...original.info, name: 123 },
      }).valid,
    ).toBe(false);
    expect(
      validateCharacterSpec({
        ...original,
        info: { ...original.info, name: "🌱".repeat(40) },
      }).valid,
    ).toBe(true);
  });
});

it("preserves an unsafe rejected draft and keeps preview sandboxed", async () => {
  const record = {
    id: "owned",
    version: 1,
    spec: structuredClone(defaultGuideFixture),
  };
  vi.mocked(characterLibrary.list).mockResolvedValue([record]);
  vi.mocked(characterLibrary.edit).mockRejectedValue(
    new Error("screening_unavailable"),
  );
  render(<CharacterLibrary />);
  fireEvent.click(
    await screen.findByRole("button", {
      name: `Edit ${record.spec.info.name}`,
    }),
  );
  const bio = screen.getByLabelText("Bio");
  fireEvent.change(bio, { target: { value: "My private draft" } });
  fireEvent.click(screen.getByRole("button", { name: "Save free edit" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "screening_unavailable",
  );
  expect(bio).toHaveValue("My private draft");
  expect(screen.getByTitle("Character preview")).toHaveAttribute("sandbox", "");
  expect(characterLibrary.edit).toHaveBeenCalledWith(
    record,
    expect.objectContaining({
      info: expect.objectContaining({ bio: "My private draft" }),
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() =>
    expect(
      screen.getByRole("heading", { name: "Private characters" }),
    ).toHaveFocus(),
  );
});

it("rejects duplicate Cast roles and recovers empty selection for free", async () => {
  vi.mocked(characterLibrary.list).mockResolvedValue([]);
  vi.mocked(characterLibrary.cast).mockResolvedValue({
    guide: "default-guide",
    supporting: [],
  });
  vi.mocked(characterLibrary.select).mockResolvedValue({
    guide: "",
    supporting: [],
  });
  render(<CourseCast courseId="course" />);
  const guide = await screen.findByLabelText("Main guide");
  fireEvent.change(screen.getByLabelText("Supporting character 1"), {
    target: { value: "default-guide" },
  });
  expect(
    screen.getByRole("button", { name: "Save Course Cast" }),
  ).toBeDisabled();
  expect(screen.getByRole("alert")).toHaveTextContent("distinct");
  fireEvent.change(guide, { target: { value: "" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Course Cast" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Saved");
  expect(characterLibrary.select).toHaveBeenCalledWith("course", {
    guide: "",
    supporting: [],
  });
});
