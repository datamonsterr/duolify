import { describe, expect, it } from "vitest";
import {
  controlsReducer,
  entryReducer,
  initialControls,
  rotationAction,
} from "./presentation";

describe("entryReducer", () => {
  it("enters from framed and records the trigger", () => {
    expect(entryReducer(null, { type: "enter", trigger: "control" })).toEqual({
      trigger: "control",
    });
  });
  it("ignores a second enter", () => {
    const s = { trigger: "control" as const };
    expect(entryReducer(s, { type: "enter", trigger: "rotation" })).toBe(s);
  });
  it("exits by control, back and escape whatever the trigger", () => {
    for (const reason of ["control", "back", "escape"] as const)
      expect(
        entryReducer({ trigger: "control" }, { type: "exit", reason }),
      ).toBeNull();
  });
  it("exits on rotation back only when entered by rotation", () => {
    const byControl = { trigger: "control" as const };
    expect(entryReducer(byControl, { type: "exit", reason: "rotation" })).toBe(
      byControl,
    );
    expect(
      entryReducer(
        { trigger: "rotation" },
        { type: "exit", reason: "rotation" },
      ),
    ).toBeNull();
  });
});

describe("rotationAction", () => {
  const base = {
    setting: true,
    coarse: true,
    landscape: true,
    immersive: null,
  };
  it("enters when landscape, setting on, touch device, framed", () => {
    expect(rotationAction(base)).toBe("enter");
  });
  it("does nothing with the setting off", () => {
    expect(rotationAction({ ...base, setting: false })).toBeNull();
  });
  it("does nothing on a desktop pointer", () => {
    expect(rotationAction({ ...base, coarse: false })).toBeNull();
  });
  it("exits when turned back to portrait after entering by rotation", () => {
    expect(
      rotationAction({
        ...base,
        landscape: false,
        immersive: { trigger: "rotation" },
      }),
    ).toBe("exit");
  });
  it("keeps immersive when turned back after entering by control", () => {
    expect(
      rotationAction({
        ...base,
        landscape: false,
        immersive: { trigger: "control" },
      }),
    ).toBeNull();
  });
  it("does not re-enter while already immersive", () => {
    expect(
      rotationAction({ ...base, immersive: { trigger: "control" } }),
    ).toBeNull();
  });
});

describe("controlsReducer", () => {
  it("toggles the control layer", () => {
    let s = initialControls(false);
    s = controlsReducer(s, { type: "toggle" });
    expect(s.visible).toBe(true);
    s = controlsReducer(s, { type: "toggle" });
    expect(s.visible).toBe(false);
  });
  it("locking hides everything and ignores gestures", () => {
    let s = controlsReducer(initialControls(true), { type: "lock" });
    expect(s).toMatchObject({ locked: true, visible: false, list: false });
    s = controlsReducer(s, { type: "toggle" });
    s = controlsReducer(s, { type: "show" });
    expect(s.visible).toBe(false);
  });
  it("unlock restores the control layer", () => {
    let s = controlsReducer(initialControls(true), { type: "lock" });
    s = controlsReducer(s, { type: "unlock" });
    expect(s).toMatchObject({ locked: false, visible: true });
  });
  it("opening the slide list shows controls; hiding closes the list", () => {
    let s = controlsReducer(initialControls(false), { type: "openList" });
    expect(s).toMatchObject({ visible: true, list: true });
    s = controlsReducer(s, { type: "hide" });
    expect(s).toMatchObject({ visible: false, list: false });
  });
});
