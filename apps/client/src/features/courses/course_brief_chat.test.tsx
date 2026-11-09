import {
  act,
  render,
  screen,
  fireEvent,
  waitFor,
} from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { CourseBrief } from "./CourseBrief";
import { emptyBrief, seedBrief } from "./brief";
import { configureAnalytics } from "../../lib/analytics";
import posthog from "posthog-js/dist/module.no-external";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: () => false,
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));
const learnerId = "11111111-1111-4111-8111-111111111111";
beforeEach(() => {
  sessionStorage.clear();
  vi.clearAllMocks();
  configureAnalytics({ learnerId, optedOut: false, platform: "web" });
});
function api() {
  return {
    config: vi.fn().mockResolvedValue({
      enabled: true,
      credits: 0,
      pricing_version: "usd-dev-unapproved",
    }),
    chat: vi.fn().mockRejectedValue(new Error("outage")),
    confirm: vi.fn().mockResolvedValue({
      id: "revision",
      content_hash: "hash",
      brief: emptyBrief(),
    }),
  };
}
describe("course brief", () => {
  it("carries the current answer into form on failure and invalidates confirmation on edit", async () => {
    const service = api();
    render(
      <CourseBrief
        learnerId={learnerId}
        service={service}
        initialBrief={{ ...emptyBrief(), topic: "Astronomy" }}
      />,
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Send answer" })).toBeDefined(),
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Your answer" }), {
      target: { value: "Moon phases" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send answer" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText("Topic", { exact: true })).toHaveValue(
      "Moon phases",
    );
    fireEvent.change(screen.getByLabelText("Skip topics", { exact: true }), {
      target: { value: "History" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Confirm brief" }));
    await screen.findByText(/Brief confirmed/);
    expect(service.confirm).toHaveBeenCalledWith(
      expect.objectContaining({ topic: "Moon phases", skip_topics: "History" }),
    );
    fireEvent.change(screen.getByLabelText("Maths", { exact: true }), {
      target: { value: "Light maths" },
    });
    expect(screen.queryByText(/Brief confirmed/)).not.toBeInTheDocument();
    expect(JSON.stringify(vi.mocked(posthog.capture).mock.calls)).not.toContain(
      "Moon phases",
    );
    expect(vi.mocked(posthog.capture).mock.calls).toEqual(
      expect.arrayContaining([
        [
          "ui_control_activated",
          expect.objectContaining({
            control_id: "confirm_brief",
            distinct_id: learnerId,
            source: "client",
          }),
        ],
      ]),
    );
  });
  it("announces each completed assistant message once and opens form when gated", async () => {
    const service = api();
    service.chat.mockResolvedValue({
      brief: { ...emptyBrief(), topic: "Moon" },
      message: "What is your goal?",
      next_step: "goal",
      is_follow_up: false,
    });
    const { rerender } = render(
      <CourseBrief learnerId={learnerId} service={service} />,
    );
    await waitFor(() => expect(service.config).toHaveBeenCalled());
    fireEvent.change(screen.getByRole("textbox", { name: "Your answer" }), {
      target: { value: "Moon" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send answer" }));
    await waitFor(() =>
      expect(screen.getByRole("log")).toHaveTextContent("What is your goal?"),
    );
    rerender(<CourseBrief learnerId={learnerId} service={service} />);
    expect(
      screen.getByRole("log").textContent?.match(/What is your goal\?/g),
    ).toHaveLength(1);
  });
  it("falls back on a stalled network without losing the in-flight answer", async () => {
    vi.useFakeTimers();
    try {
      const service = api();
      service.chat.mockImplementation(() => new Promise(() => {}));
      render(<CourseBrief learnerId={learnerId} service={service} />);
      await act(async () => {});
      fireEvent.change(screen.getByRole("textbox", { name: "Your answer" }), {
        target: { value: "Night sky" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Send answer" }));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(15001);
      });
      expect(screen.getByRole("alert")).toHaveTextContent(
        "Your answers are kept",
      );
      expect(screen.getByLabelText("Topic", { exact: true })).toHaveValue(
        "Night sky",
      );
    } finally {
      vi.useRealTimers();
    }
  });
  it("seeds only confirmed profile fields and never invents skipped context", () => {
    const profile = {
      topic: "Astronomy",
      goal: "For fun",
      level: "beginner",
      locale: "vi-VN",
      setup_completed: false,
    };
    expect(seedBrief(profile)).toEqual(emptyBrief());
    expect(seedBrief({ ...profile, setup_completed: true })).toEqual({
      ...emptyBrief(),
      topic: "Astronomy",
      goal: "For fun",
      level: "beginner",
      language: "vi-VN",
    });
    expect(
      seedBrief({
        ...profile,
        topic: "",
        goal: "",
        level: "",
        setup_completed: true,
      }),
    ).toEqual(emptyBrief());
  });
  it("keeps skipped setup empty and allows form confirmation while chat is disabled", async () => {
    const service = api();
    service.config.mockResolvedValue({
      enabled: false,
      credits: null,
      pricing_version: "",
    });
    configureAnalytics({ learnerId, optedOut: true, platform: "web" });
    render(<CourseBrief learnerId={learnerId} service={service} />);
    await screen.findByLabelText("Topic", { exact: true });
    expect(screen.getByLabelText("Goal", { exact: true })).toHaveValue("");
    fireEvent.change(screen.getByLabelText("Topic", { exact: true }), {
      target: { value: "Physics" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Confirm brief" }));
    await screen.findByText(/Brief confirmed/);
    expect(service.chat).not.toHaveBeenCalled();
    expect(posthog.capture).not.toHaveBeenCalled();
  });
});
