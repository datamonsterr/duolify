import { describe, expect, it } from "vitest";
import { handleComposerShortcut, matchesShortcut } from "./matcher";

function key(input: KeyboardEventInit) {
  return new KeyboardEvent("keydown", input);
}

describe("shortcut matching", () => {
  it("keeps letters physical and matches symbols as produced", () => {
    expect(matchesShortcut("play_pause", key({ code: "KeyK", key: "ن" }))).toBe(
      true,
    );
    expect(matchesShortcut("bookmark", key({ code: "KeyB", key: "لا" }))).toBe(
      true,
    );
    expect(matchesShortcut("show_help", key({ code: "KeyM", key: "?" }))).toBe(
      true,
    );
    expect(
      matchesShortcut(
        "show_help",
        key({ code: "Slash", key: "?", shiftKey: true }),
      ),
    ).toBe(true);
    expect(
      matchesShortcut(
        "speed_up",
        key({ code: "Backslash", key: ">", shiftKey: true }),
      ),
    ).toBe(true);
    expect(matchesShortcut("play_pause", key({ code: "KeyX", key: "k" }))).toBe(
      false,
    );
  });

  it("requires only the platform modifier for modifier shortcuts", () => {
    expect(
      matchesShortcut("undo", key({ code: "KeyZ", metaKey: true }), "mac"),
    ).toBe(true);
    expect(
      matchesShortcut("undo", key({ code: "KeyZ", ctrlKey: true }), "mac"),
    ).toBe(false);
    expect(
      matchesShortcut("undo", key({ code: "KeyZ", ctrlKey: true }), "other"),
    ).toBe(true);
    expect(
      matchesShortcut(
        "undo",
        key({ code: "KeyZ", ctrlKey: true, shiftKey: true }),
        "other",
      ),
    ).toBe(false);
    expect(
      matchesShortcut("play_pause", key({ code: "KeyK", altKey: true })),
    ).toBe(false);
  });

  it("ignores IME, repeats for toggles and text field descendants", () => {
    expect(
      matchesShortcut("show_help", key({ key: "?", isComposing: true })),
    ).toBe(false);
    expect(
      matchesShortcut(
        "show_help",
        Object.defineProperty(key({ key: "?" }), "keyCode", { value: 229 }),
      ),
    ).toBe(false);
    expect(
      matchesShortcut("play_pause", key({ code: "KeyK", repeat: true })),
    ).toBe(false);
    expect(
      matchesShortcut(
        "seek_forward_5",
        key({ key: "ArrowRight", repeat: true }),
      ),
    ).toBe(true);
    const search = document.createElement("div");
    search.role = "searchbox";
    const inside = search.appendChild(document.createElement("span"));
    expect(
      matchesShortcut("show_help", key({ key: "?" }), "other", inside),
    ).toBe(false);
    expect(
      matchesShortcut("dismiss", key({ key: "Escape" }), "other", inside),
    ).toBe(true);
  });

  it("leaves Space to focused controls and navigation keys to sliders", () => {
    const space = key({ key: " " });
    for (const element of ["button", "a", "input"]) {
      const control = document.createElement(element);
      control.setAttribute("href", "#");
      expect(matchesShortcut("play_pause", space, "other", control)).toBe(
        false,
      );
    }
    const slider = document.createElement("div");
    slider.role = "slider";
    expect(
      matchesShortcut(
        "seek_forward_5",
        key({ key: "ArrowRight" }),
        "other",
        slider,
      ),
    ).toBe(false);
    expect(
      matchesShortcut(
        "play_pause",
        space,
        "other",
        document.createElement("section"),
      ),
    ).toBe(true);
  });

  it("matches eligible composer Enter without treating newline as send", () => {
    const text = document.createElement("textarea");
    expect(
      matchesShortcut(
        "composer_send",
        key({ key: "Enter" }),
        "other",
        text,
        true,
      ),
    ).toBe(true);
    expect(
      matchesShortcut(
        "composer_send",
        key({ key: "Enter", shiftKey: true }),
        "other",
        text,
        true,
      ),
    ).toBe(false);
    expect(
      matchesShortcut(
        "composer_send",
        key({ key: "Enter" }),
        "other",
        text,
        false,
      ),
    ).toBe(false);
    expect(
      matchesShortcut(
        "composer_send",
        key({ key: "Enter", metaKey: true }),
        "mac",
        text,
        false,
      ),
    ).toBe(true);
    expect(
      matchesShortcut(
        "composer_send",
        key({ key: "Enter", isComposing: true }),
        "other",
        text,
        true,
      ),
    ).toBe(false);
  });

  it("keeps empty, pending and composing drafts in the composer", () => {
    const text = document.createElement("textarea");
    let sends = 0;
    text.addEventListener("keydown", (event) => {
      handleComposerShortcut(event, text.value, text.disabled, () => sends++);
    });
    text.dispatchEvent(key({ key: "Enter" }));
    text.value = "Draft";
    text.disabled = true;
    text.dispatchEvent(key({ key: "Enter" }));
    text.disabled = false;
    text.dispatchEvent(key({ key: "Enter", isComposing: true }));
    expect(sends).toBe(0);
    text.dispatchEvent(key({ key: "Enter" }));
    expect(sends).toBe(1);
  });
});
