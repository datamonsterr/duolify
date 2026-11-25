import { useEffect, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { CharacterSelector, type Tab } from "./CharacterSelector";
import { applyDraftPatch, defaultDraft } from "./draft";
import { CharacterStage } from "./CharacterStage";
import type { BodyKind, CharacterSpecV2 } from "./types-v2";
import { validateSpecV2 } from "./schema-v2";
import { CharacterCreator } from "./CharacterCreator";
import { characterLibrary } from "./library";

vi.mock("./library", () => ({
  characterLibrary: { createV2: vi.fn(), editV2: vi.fn() },
}));

let latest: CharacterSpecV2;
function Host({ kind = "human" as BodyKind, initialTab = "kind" as Tab }) {
  const [spec, setSpec] = useState(() => defaultDraft(kind));
  const [tab, setTab] = useState<Tab>(initialTab);
  useEffect(() => {
    latest = spec;
  });
  return (
    <>
      <CharacterStage spec={spec} />
      <CharacterSelector
        spec={spec}
        tab={tab}
        onTab={setTab}
        onReplace={setSpec}
        onPatch={(p) => {
          const r = applyDraftPatch(spec, p);
          if (r.ok) setSpec(r.spec);
        }}
      />
    </>
  );
}
const group = (name: string) => screen.getByRole("group", { name });
const pick = (g: string, option: string) =>
  fireEvent.click(within(group(g)).getByRole("radio", { name: option }));
const tab = (name: string) =>
  fireEvent.click(screen.getByRole("tab", { name }));

describe("selector widgets", () => {
  it("kind tiles switch the four kinds and keep the draft valid", () => {
    render(<Host />);
    for (const k of ["Animal", "Robot", "Fantasy", "Human"]) {
      pick("Kind", k);
      expect(validateSpecV2(latest).valid).toBe(true);
      expect(latest.body.kind).toBe(k.toLowerCase());
    }
  });

  it.each([
    ["Look", "Age", "Child"],
    ["Look", "Face", "Round"],
    ["Look", "Hair style", "Bun"],
    ["Look", "Head covering", "Beanie"],
    ["Look", "Eyes", "Wide"],
    ["Look", "Eyebrows", "Straight"],
    ["Look", "Facial hair", "Full beard"],
    ["Look", "Glasses", "Round"],
    ["Look", "Build", "Sturdy"],
    ["Outfit", "Top", "Hoodie"],
    ["Outfit", "Pattern", "Stripes"],
  ])("%s tab: %s -> %s updates the draft", (tabName, legend, option) => {
    render(<Host />);
    tab(tabName);
    const radio = within(group(legend)).queryByRole("radio", { name: option });
    if (!radio) throw new Error(`no ${option} in ${legend}`);
    fireEvent.click(radio);
    expect(radio).toBeChecked();
    expect(validateSpecV2(latest).valid).toBe(true);
  });

  it("skin tone is a swatch picker with no race or ethnicity field", () => {
    render(<Host />);
    tab("Look");
    const skin = group("Skin tone");
    expect(within(skin).getAllByRole("radio").length).toBeGreaterThanOrEqual(8);
    fireEvent.click(within(skin).getAllByRole("radio")[0]!);
    expect(screen.queryByText(/race|ethnic|nationality/i)).toBeNull();
    expect(screen.queryByRole("group", { name: /race|ethnic/i })).toBeNull();
  });

  it("colour pickers offer presets and a custom colour", () => {
    render(<Host />);
    tab("Look");
    const hair = group("Hair colour");
    fireEvent.click(within(hair).getAllByRole("radio")[1]!);
    expect(latest.body.kind === "human" && latest.body.hair.color).toMatch(
      /^#[0-9A-F]{6}$/i,
    );
    fireEvent.change(within(hair).getByLabelText("Custom colour"), {
      target: { value: "#112233" },
    });
    expect(latest.body.kind === "human" && latest.body.hair.color).toBe(
      "#112233",
    );
  });

  it("accessories toggle on and off and take a colour", () => {
    render(<Host />);
    tab("Outfit");
    fireEvent.click(screen.getByRole("checkbox", { name: "Scarf" }));
    expect(latest.clothes.accessories.map((a) => a.kind)).toEqual(["scarf"]);
    expect(screen.getByRole("group", { name: "Scarf colour" })).toBeTruthy();
    fireEvent.click(screen.getByRole("checkbox", { name: "Scarf" }));
    expect(latest.clothes.accessories).toEqual([]);
  });

  it("name, gender label, personality and voice edit the draft", () => {
    render(<Host />);
    tab("About");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Mira" },
    });
    fireEvent.change(screen.getByLabelText("Gender or label (optional)"), {
      target: { value: "non-binary" },
    });
    fireEvent.change(screen.getByLabelText("Personality"), {
      target: { value: "Patient and playful." },
    });
    pick("Voice", "Supporter");
    expect(latest.info.name).toBe("Mira");
    expect(latest.identity.label).toBe("non-binary");
    expect(latest.personality.persona_prompt).toBe("Patient and playful.");
    expect(latest.voice_key).toBe("voice-provisional-supporter");
  });

  it("pronouns are chosen from tiles, with a custom entry", () => {
    render(<Host />);
    tab("About");
    pick("Pronouns", "She/her");
    expect(latest.identity.pronouns).toBe("she/her");
    pick("Pronouns", "They/them");
    expect(latest.identity.pronouns).toBe("they/them");
    pick("Pronouns", "Other");
    fireEvent.change(screen.getByLabelText("Your pronouns"), {
      target: { value: "xe/xem" },
    });
    expect(latest.identity.pronouns).toBe("xe/xem");
    pick("Pronouns", "Not set");
    expect(latest.identity.pronouns).toBe("");
  });

  it("non-human kinds hide human-only fields and show their own", () => {
    render(<Host kind="robot" initialTab="look" />);
    expect(screen.queryByRole("group", { name: "Hair style" })).toBeNull();
    expect(group("Antenna")).toBeTruthy();
    expect(group("Moves by")).toBeTruthy();
  });

  it("every tile is a radio with a name, and every group is labelled", () => {
    render(<Host />);
    for (const name of ["Kind", "Look", "Outfit", "About"]) {
      tab(name);
      for (const r of screen.getAllByRole("radio"))
        expect(r).toHaveAccessibleName();
      for (const g of screen.getAllByRole("group"))
        expect(g).toHaveAccessibleName();
    }
  });
});

describe("live preview", () => {
  it("avatar and full-body previews both update on every change", () => {
    render(<Host kind="animal" />);
    const avatar = () => screen.getByTestId("preview-avatar").innerHTML;
    const body = () => screen.getByTestId("preview-body").innerHTML;
    const a0 = avatar();
    const b0 = body();
    tab("Look");
    fireEvent.change(
      within(group("Main colour")).getByLabelText("Custom colour"),
      {
        target: { value: "#5B8DEF" },
      },
    );
    expect(avatar()).not.toBe(a0);
    expect(body()).not.toBe(b0);
    expect(avatar()).toContain("#5B8DEF");
    expect(body()).toContain("#5B8DEF");
  });

  it("each preview has an accessible description that follows the draft", () => {
    render(<Host />);
    const label = () =>
      screen.getAllByRole("img")[0]!.getAttribute("aria-label");
    const before = label();
    pick("Kind", "Robot");
    expect(label()).not.toBe(before);
  });
});

describe("motion and layout", () => {
  it("the stage does not animate under reduced motion", () => {
    window.matchMedia = ((q: string) => ({
      matches: q.includes("reduce"),
      media: q,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      onchange: null,
      dispatchEvent: () => false,
    })) as never;
    const { container } = render(<Host />);
    expect(container.querySelector("[style*='animation']")).toBeNull();
    expect(container.querySelector("animate, animateTransform")).toBeNull();
  });

  it("tabs wrap in one scroll-free row set and tiles stay touch sized", () => {
    // jsdom has no layout: assert the CSS contract instead.
    const css = readCss();
    expect(css).toMatch(
      /\.cv2-tile[^}]*min-height:\s*(4[4-9]|[5-9]\d)px|--size-target/,
    );
    expect(css).toMatch(/prefers-reduced-motion:\s*reduce/);
    expect(css).toMatch(/@media \(min-width: 900px\)/);
  });
});

function readCss() {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const fs = require("node:fs") as typeof import("node:fs");
  return fs.readFileSync(`${__dirname}/characters.css`, "utf8");
}

describe("draft preserved on error", () => {
  it("a failed save keeps every choice, shows an alert and allows retry", async () => {
    vi.mocked(characterLibrary.createV2)
      .mockRejectedValueOnce(new Error("Try again in a moment."))
      .mockResolvedValueOnce({ id: "c1", version: 1 } as never);
    const onSaved = vi.fn();
    render(<CharacterCreator onSaved={onSaved} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("radio", { name: "Robot" }));
    tab("About");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Bolt" },
    });
    const create = screen.getByRole("button", { name: "Create" });
    fireEvent.click(create);
    expect(await screen.findByRole("alert")).toHaveTextContent("Try again");
    expect(screen.getByLabelText("Name")).toHaveValue("Bolt");
    tab("Kind");
    expect(screen.getByRole("radio", { name: "Robot" })).toBeChecked();
    tab("About");
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await vi.waitFor(() => expect(onSaved).toHaveBeenCalled());
    const sent = vi.mocked(characterLibrary.createV2).mock.calls[1]![0];
    expect(sent.body.kind).toBe("robot");
    expect(sent.info.name).toBe("Bolt");
  });

  it("a rejected patch leaves the draft unchanged and says why", () => {
    const r = applyDraftPatch(defaultDraft("human"), {
      body: { kind: "robot" },
    });
    expect(r.ok).toBe(false);
  });
});
