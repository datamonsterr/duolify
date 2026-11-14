import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { session } from "../accounts/session";
import { InviteFriends } from "./InviteFriends";
import { ReceivedInvites } from "./ReceivedInvites";
import { inviteFailure, inviteMessages } from "./invites";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);

const rejected = (status: number, code?: string) =>
  Object.assign(new Error("server text"), {
    status,
    details: { error: code },
  });
const friends = {
  friends: [
    {
      id: "f1",
      username: "sam",
      display_name: "Sam",
      avatar: { initial: "S" },
      since: "2026-10-01T00:00:00Z",
    },
    {
      id: "f2",
      username: "kai",
      display_name: "Kai",
      avatar: { initial: "K" },
      since: "2026-10-01T00:00:00Z",
    },
  ],
};
const person = (username: string) => ({
  username,
  display_name: username.toUpperCase(),
  avatar: { initial: username[0].toUpperCase() },
});
const sentRow = (id: string, username: string, status = "pending") => ({
  id,
  status,
  method: "username",
  created_at: "2026-11-14T10:00:00Z",
  expires_at: "2026-12-14T10:00:00Z",
  recipient: person(username),
});

/** Routes each call by path so the order of requests does not matter. */
function serve(
  extra: (path: string, method: string, body: unknown) => unknown = () =>
    undefined,
  sent: unknown[] = [],
) {
  request.mockImplementation(async (path, method = "GET", body) => {
    if (!path) return undefined as never; // vitest cleanup probes the mock
    const custom = extra(path, method, body);
    if (custom !== undefined) {
      if (custom instanceof Error) throw custom;
      return custom as never;
    }
    if (path === "/v1/social/friends") return friends as never;
    if (path === "/v1/courses/c1/invites" && method === "GET")
      return { invites: sent } as never;
    throw new Error(`unexpected ${method} ${path}`);
  });
}

beforeEach(() => request.mockReset());

describe("invite failures", () => {
  it("maps codes and status to neutral kinds", () => {
    expect(inviteFailure(rejected(409, "course_not_shared"))).toBe(
      "not_shared",
    );
    expect(inviteFailure(rejected(404, "invite_unavailable"))).toBe(
      "unavailable",
    );
    expect(inviteFailure(rejected(429, "invite_rate_limited"))).toBe("limited");
    expect(inviteFailure(rejected(409, "invite_duplicate"))).toBe("duplicate");
    expect(inviteFailure(new TypeError("Failed to fetch"))).toBe("offline");
    expect(inviteMessages.unavailable).toBe("We couldn't send that invite.");
  });
});

describe("invite sheet", () => {
  const open = (shared = true, onShareFirst = vi.fn()) =>
    render(
      <InviteFriends
        courseId="c1"
        shared={shared}
        onShareFirst={onShareFirst}
      />,
    );

  it("asks a private owner to share by link first and sends nothing", async () => {
    serve();
    const onShareFirst = vi.fn();
    open(false, onShareFirst);
    expect(screen.getByTestId("invite-private-prompt")).toHaveTextContent(
      "Anyone with the link",
    );
    expect(screen.queryByRole("button", { name: "Send invite" })).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Share with link first" }),
    );
    expect(onShareFirst).toHaveBeenCalled();
    expect(request).not.toHaveBeenCalled();
  });

  it("sends by username (leading @ and case ignored) and lists it as pending", async () => {
    let list: unknown[] = [];
    serve((path, method, body) => {
      if (path === "/v1/courses/c1/invites" && method === "POST") {
        expect(body).toMatchObject({ username: "sam" });
        list = [sentRow("i1", "sam")];
        return {
          id: "i1",
          status: "pending",
          expires_at: "2026-12-14T10:00:00Z",
        };
      }
      if (path === "/v1/courses/c1/invites") return { invites: list };
    });
    open();
    fireEvent.change(await screen.findByLabelText("Friend's username"), {
      target: { value: "@Sam " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send invite" }));
    expect(await screen.findByTestId("invite-notice")).toHaveTextContent(
      "Invite sent",
    );
    expect(await screen.findByTestId("sent-invite-sam")).toHaveTextContent(
      "Pending",
    );
    expect(screen.getByLabelText("Friend's username")).toHaveValue("");
  });

  it("invites a friend from the picker with friendship_id", async () => {
    serve((path, method, body) => {
      if (path === "/v1/courses/c1/invites" && method === "POST") {
        expect(body).toMatchObject({ friendship_id: "f2" });
        return { id: "i2", status: "pending", expires_at: "x" };
      }
    });
    open();
    fireEvent.click(await screen.findByTestId("invite-friend-kai"));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "/v1/courses/c1/invites",
        "POST",
        expect.objectContaining({ friendship_id: "f2" }),
      ),
    );
    expect(await screen.findByTestId("invite-notice")).toHaveTextContent(
      "Invite sent",
    );
  });

  it("keeps already invited friends from being invited twice", async () => {
    serve(() => undefined, [sentRow("i1", "sam")]);
    open();
    expect(await screen.findByTestId("invite-friend-sam")).toBeDisabled();
    expect(screen.getByTestId("invite-friend-kai")).toBeEnabled();
  });

  it.each([
    [rejected(404, "invite_unavailable"), "We couldn't send that invite."],
    [rejected(429, "invite_rate_limited"), "Daily invite limit reached"],
    [rejected(409, "invite_duplicate"), "already have a pending invite"],
    [new TypeError("Failed to fetch"), "Check your connection"],
  ])("shows a neutral message for %#", async (error, text) => {
    serve((path, method) =>
      path === "/v1/courses/c1/invites" && method === "POST"
        ? error
        : undefined,
    );
    open();
    fireEvent.change(await screen.findByLabelText("Friend's username"), {
      target: { value: "ghost" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send invite" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(text);
    expect(screen.queryByTestId("invite-notice")).toBeNull();
  });

  it("reuses one operation key across an offline retry", async () => {
    const keys: unknown[] = [];
    let fail = true;
    serve((path, method, body) => {
      if (path === "/v1/courses/c1/invites" && method === "POST") {
        keys.push((body as { operation_key: string }).operation_key);
        if (fail) return new TypeError("Failed to fetch");
        return { id: "i1", status: "pending", expires_at: "x" };
      }
    });
    open();
    fireEvent.change(await screen.findByLabelText("Friend's username"), {
      target: { value: "sam" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send invite" }));
    await screen.findByRole("alert");
    fail = false;
    fireEvent.click(screen.getByRole("button", { name: "Send invite" }));
    await screen.findByTestId("invite-notice");
    expect(keys).toHaveLength(2);
    expect(keys[0]).toBe(keys[1]);
  });

  it("revokes a pending invite from the sent list", async () => {
    serve(
      (path, method) => {
        if (path === "/v1/invites/i1/revoke" && method === "POST")
          return { status: "revoked" };
      },
      [sentRow("i1", "sam")],
    );
    open();
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke invite for @sam" }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("sent-invite-sam")).toHaveTextContent(
        "Revoked",
      ),
    );
    expect(
      screen.queryByRole("button", { name: "Revoke invite for @sam" }),
    ).toBeNull();
  });

  it("shows a retryable error when the lists cannot load", async () => {
    request.mockImplementation(async (path) => {
      if (path) throw new TypeError("Failed to fetch");
    });
    open();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Check your connection",
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});

describe("received invites", () => {
  const received = (id: string, title: string, read = false) => ({
    id,
    created_at: "2026-11-14T10:00:00Z",
    expires_at: "2026-12-14T10:00:00Z",
    read,
    course: { title },
    sender: person("mina"),
  });

  it("shows only the course title and sender, with an empty state", () => {
    const { rerender } = render(
      <ReceivedInvites items={[]} onResolved={vi.fn()} onAccepted={vi.fn()} />,
    );
    expect(screen.getByText("No course invites right now.")).toBeVisible();
    rerender(
      <ReceivedInvites
        items={[received("i1", "Rust basics")]}
        onResolved={vi.fn()}
        onAccepted={vi.fn()}
      />,
    );
    const row = screen.getByTestId("received-invite-mina-Rust basics");
    expect(row).toHaveTextContent("MINA invited you to Rust basics");
    expect(row).toHaveTextContent("New");
  });

  it("accepts once, navigates to the copy and ignores a double click", async () => {
    request.mockResolvedValue({ status: "accepted", course_id: "copy1" });
    const onAccepted = vi.fn();
    const onResolved = vi.fn();
    render(
      <ReceivedInvites
        items={[received("i1", "Rust basics")]}
        onResolved={onResolved}
        onAccepted={onAccepted}
      />,
    );
    const accept = screen.getByRole("button", { name: "Accept" });
    fireEvent.click(accept);
    fireEvent.click(accept);
    await waitFor(() => expect(onAccepted).toHaveBeenCalledWith("copy1"));
    expect(onAccepted).toHaveBeenCalledTimes(1);
    expect(request).toHaveBeenCalledTimes(1);
    expect(request).toHaveBeenCalledWith("/v1/invites/i1/accept", "POST");
    expect(onResolved).toHaveBeenCalledWith("i1");
  });

  it("declines and removes the row", async () => {
    request.mockResolvedValue({ status: "declined" });
    const onResolved = vi.fn();
    render(
      <ReceivedInvites
        items={[received("i1", "Rust basics")]}
        onResolved={onResolved}
        onAccepted={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Decline" }));
    await waitFor(() => expect(onResolved).toHaveBeenCalledWith("i1"));
    expect(request).toHaveBeenCalledWith("/v1/invites/i1/decline", "POST");
  });

  it("an unavailable invite is removed with a neutral note; offline keeps it", async () => {
    request.mockRejectedValueOnce(rejected(404, "invite_unavailable"));
    const onResolved = vi.fn();
    render(
      <ReceivedInvites
        items={[received("i1", "Rust basics")]}
        onResolved={onResolved}
        onAccepted={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Accept" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This invite isn't available any more.",
    );
    expect(onResolved).toHaveBeenCalledWith("i1");

    request.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    const again = vi.fn();
    render(
      <ReceivedInvites
        items={[received("i2", "Other")]}
        onResolved={again}
        onAccepted={vi.fn()}
      />,
    );
    fireEvent.click(screen.getAllByRole("button", { name: "Accept" })[1]);
    await waitFor(() =>
      expect(screen.getAllByRole("alert").length).toBeGreaterThan(0),
    );
    expect(again).not.toHaveBeenCalled();
  });
});
