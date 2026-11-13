import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { FriendsPage } from "./FriendsPage";
import { PrivacySettings } from "./PrivacySettings";
import { blockFailure, blockMessages, blocksPath } from "./blocks";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
vi.mock("./SocialIdentity", () => ({ SocialIdentity: () => null }));
const request = vi.mocked(session.request);
const rejected = (status: number, code = "") =>
  Object.assign(new Error("server text"), { status, code });
const person = (username: string, extra: object = {}) => ({
  username,
  display_name: `Display ${username}`,
  avatar: { initial: username[0].toUpperCase() },
  ...extra,
});
const friend = (username: string) =>
  person(username, { id: `f-${username}`, since: "2026-11-01T00:00:00Z" });
const blockedRow = (username: string) =>
  person(username, { blocked_at: "2026-11-10T00:00:00Z" });

function serve(
  state: { friends?: unknown[]; blocks?: unknown[] } = {},
  extra: (path: string, method: string, body?: unknown) => unknown = () =>
    undefined,
) {
  request.mockImplementation(
    async (path: string, method = "GET", body?: unknown) => {
      const custom = extra(path, method, body);
      if (custom !== undefined) return custom as never;
      if (path === "/v1/social/friends")
        return { friends: state.friends ?? [] } as never;
      if (path === "/v1/social/friend-requests" && method === "GET")
        return { incoming: [], outgoing: [] } as never;
      if (path === blocksPath && method === "GET")
        return { blocks: state.blocks ?? [] } as never;
      return { status: "ok" } as never;
    },
  );
}
const calls = (method: string, path: string) =>
  request.mock.calls.filter(([p, m = "GET"]) => p === path && m === method);

describe("block helpers", () => {
  it("classifies failures by status and code, never by server text", () => {
    expect(blockFailure(rejected(404, "block_unavailable"))).toBe(
      "unavailable",
    );
    expect(blockFailure(rejected(409, "block_limit"))).toBe("limit");
    expect(blockFailure(rejected(429))).toBe("limited");
    expect(blockFailure(rejected(503))).toBe("offline");
    expect(blockFailure(new TypeError("Failed to fetch"))).toBe("offline");
  });
  it("keeps copy neutral: no hint why a person can't be blocked", () => {
    expect(Object.keys(blockMessages).sort()).toEqual(
      ["limit", "limited", "offline", "unavailable"].sort(),
    );
    for (const text of Object.values(blockMessages))
      expect(text.length).toBeGreaterThan(10);
    expect(blockMessages.unavailable).toBe("We couldn't block that person.");
    expect(blockMessages.unavailable.toLowerCase()).not.toMatch(
      /exist|found|hidden|private|username/,
    );
  });
});

describe("Block a friend", () => {
  beforeEach(() => request.mockReset());

  async function openFriend(name = "kai") {
    render(<FriendsPage />);
    fireEvent.click(
      await screen.findByRole("button", { name: `Open Display ${name}` }),
    );
  }

  it("confirms with the consequence, restores focus on cancel, then blocks and reloads", async () => {
    serve({ friends: [friend("kai")] });
    await openFriend();
    const trigger = screen.getByRole("button", { name: "Block" });
    trigger.focus();
    fireEvent.click(trigger);
    const dialog = await screen.findByRole("dialog", {
      name: "Block Display kai?",
    });
    expect(dialog).toHaveTextContent("can't find you");
    expect(dialog).toHaveTextContent("friend streak");
    expect(dialog).toHaveTextContent("isn't notified");
    expect(
      within(dialog).getByRole("button", { name: "Cancel" }),
    ).toHaveFocus();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(calls("POST", blocksPath)).toHaveLength(0);

    fireEvent.click(trigger);
    const again = await screen.findByRole("dialog", {
      name: "Block Display kai?",
    });
    fireEvent.click(within(again).getByRole("button", { name: "Block" }));
    await waitFor(() => expect(calls("POST", blocksPath)).toHaveLength(1));
    expect(calls("POST", blocksPath)[0][2]).toEqual({ username: "kai" });
    // The list is reloaded from the server, which owns the truth.
    await waitFor(() =>
      expect(calls("GET", "/v1/social/friends").length).toBeGreaterThan(1),
    );
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("offers 'Only remove friend' that never creates a block", async () => {
    serve({ friends: [friend("kai")] });
    await openFriend();
    fireEvent.click(screen.getByRole("button", { name: "Block" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Block Display kai?",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Only remove friend" }),
    );
    await waitFor(() =>
      expect(calls("DELETE", "/v1/social/friends/f-kai")).toHaveLength(1),
    );
    expect(calls("POST", blocksPath)).toHaveLength(0);
  });

  it("shows neutral copy when the server refuses", async () => {
    serve({ friends: [friend("kai")] }, (path, method) => {
      if (path === blocksPath && method === "POST")
        throw rejected(404, "block_unavailable");
      return undefined;
    });
    await openFriend();
    fireEvent.click(screen.getByRole("button", { name: "Block" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Block" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "We couldn't block that person.",
    );
  });
});

describe("Blocked list", () => {
  beforeEach(() => request.mockReset());

  it("shows the count, long names without overflow, and an empty state", async () => {
    const long = "a".repeat(20);
    serve({ blocks: [blockedRow(long), blockedRow("sam")] });
    const { unmount } = render(<FriendsPage />);
    fireEvent.click(await screen.findByRole("tab", { name: "Blocked · 2" }));
    const list = screen.getByRole("list", { name: "Blocked learners" });
    expect(within(list).getAllByRole("listitem")).toHaveLength(2);
    expect(list).toHaveTextContent(`@${long}`);
    unmount();
    serve({ blocks: [] });
    render(<FriendsPage />);
    fireEvent.click(await screen.findByRole("tab", { name: "Blocked · 0" }));
    expect(screen.getByText("You haven't blocked anyone.")).toBeVisible();
    expect(screen.queryByRole("list", { name: "Blocked learners" })).toBeNull();
  });

  it("unblocks without a confirmation and reloads", async () => {
    serve({ blocks: [blockedRow("sam")] });
    render(<FriendsPage />);
    fireEvent.click(await screen.findByRole("tab", { name: "Blocked · 1" }));
    fireEvent.click(screen.getByRole("button", { name: "Unblock sam" }));
    await waitFor(() =>
      expect(calls("DELETE", `${blocksPath}/sam`)).toHaveLength(1),
    );
    await waitFor(() =>
      expect(calls("GET", blocksPath).length).toBeGreaterThan(1),
    );
  });

  it("blocks by username after a confirm, trimming a leading @", async () => {
    serve({ blocks: [] });
    render(<FriendsPage />);
    fireEvent.click(await screen.findByRole("tab", { name: "Blocked · 0" }));
    const input = screen.getByRole("textbox", { name: "Username to block" });
    const go = screen.getByRole("button", { name: "Block someone" });
    expect(go).toBeDisabled();
    fireEvent.change(input, { target: { value: "  @Lena.Two " } });
    expect(go).toBeEnabled();
    fireEvent.click(go);
    const dialog = await screen.findByRole("dialog", {
      name: "Block @Lena.Two?",
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Block" }));
    await waitFor(() => expect(calls("POST", blocksPath)).toHaveLength(1));
    expect(calls("POST", blocksPath)[0][2]).toEqual({ username: "Lena.Two" });
    expect(
      within(dialog).queryByRole("button", { name: "Only remove friend" }),
    ).toBeNull();
  });
});

describe("Course invite setting", () => {
  beforeEach(() => request.mockReset());
  const settings = {
    discoverable: true,
    request_policy: "everyone",
    show_streak_to_friends: true,
    course_invites_from: "friends",
  };

  it("saves the choice alone and keeps the old one when saving fails", async () => {
    request.mockImplementation(async (_p: string, method = "GET") => {
      if (method === "PATCH")
        return { ...settings, course_invites_from: "nobody" } as never;
      return settings as never;
    });
    render(<PrivacySettings />);
    const select = await screen.findByRole("combobox", {
      name: "Course invites from",
    });
    expect(select).toHaveValue("friends");
    expect(
      within(select)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Friends", "Everyone", "Nobody"]);
    fireEvent.change(select, { target: { value: "nobody" } });
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("/v1/social/settings", "PATCH", {
        course_invites_from: "nobody",
      }),
    );
    expect(await screen.findByText("Saved.")).toBeVisible();

    request.mockImplementation(async (_p: string, method = "GET") => {
      if (method === "PATCH") throw new Error("offline");
      return settings as never;
    });
    fireEvent.change(select, { target: { value: "everyone" } });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Couldn't save “Course invites from”",
    );
  });
});
