import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as analytics from "../../lib/analytics";
import { mobileTabs } from "../shell/navigation";
import { getHelpSections, nativeMenu, shortcutAria } from "./registry";
import { useHotkeys, useEscapeLayer } from "./useHotkeys";

describe("shortcut registry integration", () => {
  it("provides the player bindings to the focused handler, help and controls", () => {
    const togglePlayback = vi.fn();
    const previousSlide = vi.fn();
    function Player({ enabled }: { enabled: boolean }) {
      useHotkeys({
        scope: "player",
        surface: "lesson",
        singleKeyShortcuts: enabled,
        handlers: { play_pause: togglePlayback, previous_slide: previousSlide },
      });
      return (
        <section data-hotkey-player tabIndex={0} aria-label="Stage">
          <button aria-keyshortcuts={shortcutAria("play_pause", "other")}>
            Play
          </button>
        </section>
      );
    }
    const view = render(<Player enabled />);
    const stage = screen.getByRole("region", { name: "Stage" });
    const play = screen.getByRole("button", { name: "Play" });
    expect(play).toHaveAttribute("aria-keyshortcuts", "K Space");
    const sections = getHelpSections("player", false);
    expect(sections[0]?.group).toBe("playback");
    expect(sections[0]?.rows[0]?.[0]?.id).toBe("play_pause");
    expect(
      getHelpSections("player", true).flatMap((s) => s.rows).length,
    ).toBeLessThanOrEqual(24);

    stage.focus();
    fireEvent.keyDown(stage, { code: "KeyK", key: "س" });
    fireEvent.keyDown(stage, { code: "KeyK", key: "k", repeat: true });
    play.focus();
    fireEvent.keyDown(play, { code: "Space", key: " " });
    expect(togglePlayback).toHaveBeenCalledTimes(1);
    view.rerender(<Player enabled={false} />);
    stage.focus();
    fireEvent.keyDown(stage, { code: "KeyK", key: "k" });
    expect(togglePlayback).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(stage, { key: "PageUp" });
    expect(previousSlide).not.toHaveBeenCalled();
    fireEvent.keyDown(stage, { key: "ArrowLeft", shiftKey: true });
    expect(previousSlide).toHaveBeenCalledOnce();
  });

  it("takes native destination order and accelerators from the registry", () => {
    const destinations = nativeMenu.filter(
      ({ menu }) => menu.group === "navigate",
    );
    expect(destinations.map(({ id }) => id)).toEqual(
      mobileTabs.slice(0, 4).map(({ id }) => `go_${id}`),
    );
    expect(destinations.map(({ menu }) => menu.accelerator)).toEqual(
      [1, 2, 3, 4].map((number) => `CmdOrCtrl+${number}`),
    );
    const settings = nativeMenu.find(({ id }) => id === "settings");
    expect(settings?.menu.accelerator).toBe("CmdOrCtrl+Comma");
    expect(shortcutAria("settings", "mac")).toBe("Meta+Comma");
    expect(shortcutAria("settings", "other")).toBe("Control+Comma");
  });

  it("dismisses the highest layer without sending text-field analytics", () => {
    const track = vi.spyOn(analytics, "trackHotkey");
    const order: string[] = [];
    function Layers() {
      useEscapeLayer("sheet", () => order.push("sheet"), "settings");
      useEscapeLayer("tooltip", () => order.push("tooltip"), "settings");
      return <textarea aria-label="Notes" />;
    }
    render(<Layers />);
    const text = screen.getByRole("textbox");
    text.focus();
    fireEvent.keyDown(text, { key: "Escape" });
    expect(order).toEqual(["tooltip"]);
    expect(track).not.toHaveBeenCalled();
    track.mockRestore();
  });
});
