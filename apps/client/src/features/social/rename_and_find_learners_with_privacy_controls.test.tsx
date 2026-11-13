import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { AddFriend } from "./AddFriend";
import { PrivacySettings } from "./PrivacySettings";
import { SocialIdentity } from "./SocialIdentity";
import { normalizeSearch, searchFailure, searchPath } from "./search";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
const mine = {
  username: {
    username: "Maya_Learns",
    display_name: "Learner",
    avatar: { initial: "M" },
  },
  rules: { min: 3, max: 20 },
};
const person = (username: string) => ({
  username,
  display_name: `Display ${username}`,
  avatar: { initial: username[0].toUpperCase() },
});
const rejected = (status: number, message: string) =>
  Object.assign(new Error(message), { status });

describe("search query boundaries", () => {
  it.each([
    ["may", "may"],
    ["  MAY  ", "may"],
    ["@Maya.Learns", "maya.learns"],
    ["ＭＡＹ", "may"],
    ["@@may", "@may"],
    ["", ""],
  ])("normalizes %j to %j", (raw, want) => {
    expect(normalizeSearch(raw)).toBe(want);
  });
  it("encodes the query and classifies failures without trusting server text", () => {
    expect(searchPath("a_b.c")).toBe("/v1/social/search?q=a_b.c");
    expect(searchPath("ma y&x")).toBe("/v1/social/search?q=ma%20y%26x");
    expect(searchFailure(rejected(429, "x"))).toBe("limited");
    expect(searchFailure(rejected(503, "x"))).toBe("unavailable");
    expect(searchFailure(new TypeError("Failed to fetch"))).toBe("unavailable");
  });
});

describe("Add a friend search", () => {
  beforeEach(() => request.mockReset());

  it("never calls the API below 3 characters, then searches and lists only the allowed fields", async () => {
    request.mockResolvedValue({
      results: [person("maya"), person("mayan_one")],
    });
    render(<AddFriend onClose={() => {}} />);
    const input = screen.getByRole("textbox", { name: "Username" });
    expect(screen.getByText("At least 3 characters")).toBeVisible();
    fireEvent.change(input, { target: { value: "ma" } });
    fireEvent.change(input, { target: { value: "  @MA " } });
    await new Promise((resolve) => setTimeout(resolve, 350));
    expect(request).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "@May" } });
    const list = await screen.findByRole("list", { name: "Search results" });
    expect(request).toHaveBeenCalledTimes(1);
    expect(request).toHaveBeenCalledWith("/v1/social/search?q=may");
    expect(within(list).getAllByRole("listitem")).toHaveLength(2);
    expect(list).toHaveTextContent("@mayan_one");
    expect(list).toHaveTextContent("Display maya");
  });

  it("coalesces fast typing into one request and ignores a stale slower response", async () => {
    let releaseFirst: (value: unknown) => void = () => {};
    request
      .mockImplementationOnce(
        () => new Promise((resolve) => (releaseFirst = resolve)),
      )
      .mockResolvedValueOnce({ results: [person("mayb")] });
    render(<AddFriend onClose={() => {}} />);
    const input = screen.getByRole("textbox", { name: "Username" });
    fireEvent.change(input, { target: { value: "may" } });
    await waitFor(() => expect(request).toHaveBeenCalledTimes(1));
    fireEvent.change(input, { target: { value: "mayb" } });
    await screen.findByText("@mayb");
    releaseFirst({ results: [person("maya")] });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.queryByText("@maya")).toBeNull();
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("shows the same neutral empty copy for any empty result", async () => {
    request.mockResolvedValue({ results: [] });
    render(<AddFriend onClose={() => {}} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Username" }), {
      target: { value: "ghost" },
    });
    expect(
      await screen.findByText("No one found with that username"),
    ).toBeVisible();
    expect(screen.getByText(/Check the spelling/)).toBeVisible();
    expect(screen.queryByRole("list")).toBeNull();
  });

  it("recovers from an outage and explains rate limiting without auto-retrying", async () => {
    request.mockRejectedValueOnce(rejected(503, "Try again."));
    render(<AddFriend onClose={() => {}} />);
    const input = screen.getByRole("textbox", { name: "Username" });
    fireEvent.change(input, { target: { value: "maya" } });
    expect(await screen.findByRole("alert")).toHaveTextContent("unavailable");
    request.mockRejectedValueOnce(rejected(429, "Too many tries."));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Too many searches",
    );
    expect(request).toHaveBeenCalledTimes(2);
    request.mockResolvedValueOnce({ results: [person("maya")] });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("@maya")).toBeVisible();
    expect(input).toHaveValue("maya");
  });

  it("Enter searches immediately and Escape closes", async () => {
    request.mockResolvedValue({ results: [] });
    const onClose = vi.fn();
    render(<AddFriend onClose={onClose} />);
    const input = screen.getByRole("textbox", { name: "Username" });
    fireEvent.change(input, { target: { value: "maya" } });
    fireEvent.submit(input.closest("form")!);
    await screen.findByText("No one found with that username");
    fireEvent(
      screen.getByRole("dialog", { hidden: true }),
      new Event("cancel", { cancelable: true }),
    );
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("rename", () => {
  beforeEach(() => request.mockReset());

  it("keeps validation local, shows the cooldown date and keeps the draft", async () => {
    request.mockResolvedValueOnce(mine);
    render(<SocialIdentity compact />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Change username" }),
    );
    const input = screen.getByRole("textbox", { name: "New username" });
    expect(input).toHaveValue("Maya_Learns");
    expect(input).toHaveFocus();
    fireEvent.change(input, { target: { value: "a__b" } });
    fireEvent.click(screen.getByRole("button", { name: "Save username" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("3–20");
    expect(request).toHaveBeenCalledTimes(1);
    const cooldown = "You can change your username again on December 2, 2026.";
    request.mockRejectedValueOnce(rejected(409, cooldown));
    fireEvent.change(input, { target: { value: "Maya_Two" } });
    fireEvent.click(screen.getByRole("button", { name: "Save username" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(cooldown);
    expect(input).toHaveValue("Maya_Two");
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("renames with PATCH, closes the form, announces it and returns focus", async () => {
    request.mockResolvedValueOnce(mine);
    render(<SocialIdentity compact />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Change username" }),
    );
    const next = {
      ...mine,
      username: { ...mine.username, username: "Maya_Two" },
    };
    request.mockResolvedValueOnce(next);
    fireEvent.change(screen.getByRole("textbox", { name: "New username" }), {
      target: { value: "Maya_Two" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save username" }));
    expect(await screen.findByText("@Maya_Two")).toBeVisible();
    expect(request).toHaveBeenLastCalledWith("/v1/social/username", "PATCH", {
      username: "Maya_Two",
    });
    expect(
      screen.getByRole("status", { name: "Username status" }),
    ).toHaveTextContent("Username changed.");
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Change username" }),
      ).toHaveFocus(),
    );
  });

  it("offers retry on outage and cancel without a request", async () => {
    request.mockResolvedValueOnce(mine);
    render(<SocialIdentity compact />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Change username" }),
    );
    request.mockRejectedValueOnce(new Error("offline"));
    fireEvent.click(screen.getByRole("button", { name: "Save username" }));
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(screen.getByRole("button", { name: "Retry" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(request).toHaveBeenCalledTimes(2);
  });
});

describe("privacy settings with discovery disabled", () => {
  const stored = {
    discoverable: true,
    request_policy: "everyone",
    show_streak_to_friends: true,
  };
  beforeEach(() => request.mockReset());

  it("saves one setting at a time and reports success", async () => {
    request.mockResolvedValueOnce(stored);
    render(<PrivacySettings />);
    const findable = await screen.findByRole("checkbox", {
      name: "Findable by username",
    });
    request.mockResolvedValueOnce({ ...stored, discoverable: false });
    fireEvent.click(findable);
    await waitFor(() => expect(findable).not.toBeChecked());
    expect(request).toHaveBeenLastCalledWith("/v1/social/settings", "PATCH", {
      discoverable: false,
    });
    expect(
      screen.getByRole("status", { name: "Privacy settings status" }),
    ).toHaveTextContent("Saved.");
    request.mockResolvedValueOnce({
      ...stored,
      discoverable: false,
      request_policy: "nobody",
    });
    fireEvent.change(
      screen.getByRole("combobox", { name: "Requests and invites from" }),
      { target: { value: "nobody" } },
    );
    await waitFor(() =>
      expect(
        screen.getByRole("combobox", { name: "Requests and invites from" }),
      ).toHaveValue("nobody"),
    );
    expect(request).toHaveBeenLastCalledWith("/v1/social/settings", "PATCH", {
      request_policy: "nobody",
    });
  });

  it("keeps the previous value when a save fails", async () => {
    request.mockResolvedValueOnce(stored);
    render(<PrivacySettings />);
    const streak = await screen.findByRole("checkbox", {
      name: "Show my streak to friends",
    });
    request.mockRejectedValueOnce(rejected(503, "Try again."));
    fireEvent.click(streak);
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't save");
    expect(streak).toBeChecked();
    expect(streak).not.toBeDisabled();
  });

  it("recovers from a load failure", async () => {
    request.mockRejectedValueOnce(new Error("offline"));
    render(<PrivacySettings />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load");
    request.mockResolvedValueOnce(stored);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("checkbox", { name: "Findable by username" }),
    ).toBeChecked();
  });
});
