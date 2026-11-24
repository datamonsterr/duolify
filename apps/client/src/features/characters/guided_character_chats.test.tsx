import {
  fireEvent,
  configure,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GuidedCharacterCreate } from "./GuidedCharacterCreate";
import { GuidedCourseCast } from "./GuidedCourseCast";
import { characterLibrary } from "./library";
vi.mock("../../lib/analytics", () => ({ trackGuidedChatEvent: vi.fn() }));
vi.mock("./library", () => ({
  characterLibrary: {
    createV2: vi.fn(),
    list: vi.fn(),
    select: vi.fn(),
  },
}));

configure({ asyncUtilTimeout: 8000 });
vi.setConfig({ testTimeout: 30000 });
beforeEach(() => {
  localStorage.clear();
  vi.resetAllMocks();
});

const group = (name: string) => screen.getByRole("group", { name });
// Each guide line is also in the polite log, so match any.
const said = async (text: string | RegExp) =>
  expect((await screen.findAllByText(text)).length).toBeGreaterThan(0);
const next = async (label: string) =>
  fireEvent.click(await screen.findByRole("button", { name: label }));

describe("character.create", () => {
  it("builds a character from selector widgets and saves it free of Credits", async () => {
    vi.mocked(characterLibrary.createV2).mockResolvedValue({
      id: "c1",
      version: 1,
    } as never);
    const onClose = vi.fn();
    render(<GuidedCharacterCreate onClose={onClose} />);
    // Kind step: the same kind and species tiles as the editor, with live previews.
    await said("What kind of character do you want?");
    expect(await screen.findByTestId("preview-avatar")).toBeTruthy();
    expect(screen.getByTestId("preview-body")).toBeTruthy();
    fireEvent.click(screen.getByRole("radio", { name: "Animal" }));
    fireEvent.click(screen.getByRole("radio", { name: "Cat" }));
    await next("Looks good");
    // Look step
    await said("Shape how they look.");
    fireEvent.click(
      within(group("Tail")).getByRole("radio", { name: "Fluffy" }),
    );
    await next("Looks good");
    await said("Pick their outfit.");
    await next("Looks good");
    // Name step: send arrow, inline error, then valid.
    const name = await screen.findByRole("textbox", { name: /name/i });
    fireEvent.change(name, { target: { value: "x".repeat(41) } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(await screen.findByRole("alert")).toBeTruthy();
    fireEvent.change(screen.getByRole("textbox", { name: /name/i }), {
      target: { value: "Biscuit" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    // Pronouns tap, then confirm with a labelled primary button.
    fireEvent.click(await screen.findByRole("radio", { name: "They/them" }));
    await next("Send");
    await said("Create this character? It's free.");
    fireEvent.click(screen.getByRole("button", { name: "Create character" }));
    await waitFor(() =>
      expect(characterLibrary.createV2).toHaveBeenCalledTimes(1),
    );
    const doc = vi.mocked(characterLibrary.createV2).mock.calls[0]![0];
    expect(doc).toMatchObject({
      info: { name: "Biscuit" },
      identity: { pronouns: "they/them" },
      body: { kind: "animal", species: "cat", tail: "fluffy" },
    });
    await said("Character created. Nothing was spent.");
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(onClose).toHaveBeenCalled();
  });

  it("keeps every choice when saving fails and retries", async () => {
    vi.mocked(characterLibrary.createV2)
      .mockRejectedValueOnce(new Error("down"))
      .mockResolvedValueOnce({ id: "c1", version: 1 } as never);
    render(<GuidedCharacterCreate onClose={vi.fn()} />);
    fireEvent.click(await screen.findByRole("radio", { name: "Robot" }));
    await next("Looks good");
    await said("Shape how they look.");
    await next("Looks good");
    await said("Pick their outfit.");
    await next("Looks good");
    fireEvent.change(await screen.findByRole("textbox", { name: /name/i }), {
      target: { value: "Bolt" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    fireEvent.click(await screen.findByRole("radio", { name: "Not set" }));
    await next("Send");
    fireEvent.click(
      await screen.findByRole("button", { name: "Create character" }),
    );
    await said("That didn't go through. Try again.");
    // A failed save waits for the Learner; it never retries on its own.
    expect(characterLibrary.createV2).toHaveBeenCalledTimes(1);
    fireEvent.click(await screen.findByText("Try again"));
    await waitFor(() =>
      expect(characterLibrary.createV2).toHaveBeenCalledTimes(2),
    );
    expect(
      vi.mocked(characterLibrary.createV2).mock.calls[1]![0],
    ).toMatchObject({
      info: { name: "Bolt" },
      body: { kind: "robot" },
    });
  });

  it("closes with a labelled × button", async () => {
    const onClose = vi.fn();
    render(<GuidedCharacterCreate onClose={onClose} />);
    fireEvent.click(await screen.findByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalled();
  });
});

describe("character.cast", () => {
  const own = (id: string, name: string) =>
    ({
      id,
      version: 1,
      spec: { info: { name } },
      document: undefined,
    }) as never;
  it("picks a guide and supports from built-ins and own characters, then saves", async () => {
    vi.mocked(characterLibrary.list).mockResolvedValue([
      own("c1", "Mira"),
      own("c2", "Bolt"),
    ]);
    vi.mocked(characterLibrary.select).mockResolvedValue({
      guide: "c1",
      supporting: ["c2"],
    });
    render(<GuidedCourseCast courseId="course1" onClose={vi.fn()} />);
    await said("Who should guide this course?");
    fireEvent.click(await screen.findByRole("radio", { name: "Mira" }));
    await next("Choose");
    await said("Add a supporting character?");
    // The guide is not offered twice.
    await waitFor(() =>
      expect(screen.queryByRole("radio", { name: "Mira" })).toBeNull(),
    );
    fireEvent.click(await screen.findByRole("radio", { name: "Bolt" }));
    await next("Choose");
    await said(/Add one more\?/);
    fireEvent.click(await screen.findByRole("radio", { name: "No one" }));
    await next("Choose");
    await said("Save this Course Cast? It's free.");
    expect(screen.getAllByText("Mira").length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Save Cast" }));
    await waitFor(() =>
      expect(characterLibrary.select).toHaveBeenCalledWith("course1", {
        guide: "c1",
        supporting: ["c2"],
      }),
    );
    expect(await screen.findByText(/Course Cast saved/)).toBeTruthy();
  });

  it("the default cast skips the supporting steps", async () => {
    vi.mocked(characterLibrary.list).mockResolvedValue([]);
    vi.mocked(characterLibrary.select).mockResolvedValue({
      guide: "",
      supporting: [],
    });
    render(<GuidedCourseCast courseId="course1" onClose={vi.fn()} />);
    fireEvent.click(await screen.findByRole("radio", { name: "Default cast" }));
    await next("Choose");
    await said("Save this Course Cast? It's free.");
    fireEvent.click(screen.getByRole("button", { name: "Save Cast" }));
    await waitFor(() =>
      expect(characterLibrary.select).toHaveBeenCalledWith("course1", {
        guide: "",
        supporting: [],
      }),
    );
  });
});
