import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { afterEach, it, expect, vi } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { http, HttpResponse } from "msw";
import { server } from "../../test/setup";
import { configureAnalytics } from "../../lib/analytics";
import { SharedDemo } from "./SharedDemo";
import { CoursePreview } from "./CoursePreview";
import { emptyBrief } from "./brief";
import { CourseBrief } from "./CourseBrief";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
    has_opted_out_capturing: () => false,
  },
}));
const fixture = {
  kind: "shared_demo",
  version: "sky-v1",
  preview: {
    sample_outline: {
      title: "Reading the sky",
      units: [
        {
          title: "Patterns",
          lessons: [{ title: "Earth turns", goal: "Explain daily motion" }],
        },
      ],
    },
    example_slides: [
      {
        slide_id: "sky-1",
        html: '<div id="root">Sky</div>',
        narration: "Earth rotates.",
        duration_ms: 3000,
      },
    ],
  },
};
afterEach(() => {
  configureAnalytics(null);
  vi.clearAllMocks();
});
it("delivers only content-free demo analytics with UUID/common properties and opt-out", async () => {
  server.use(
    http.get("*/v1/courses/shared-demo", () => HttpResponse.json(fixture)),
  );
  const id = "11111111-1111-4111-8111-111111111111";
  configureAnalytics({ learnerId: id, optedOut: false, platform: "web" });
  const view = render(
    <SharedDemo surface="course_brief" onReturn={() => {}} />,
  );
  await screen.findByRole("heading", { name: "Reading the sky" });
  expect(posthog.capture).toHaveBeenCalledWith(
    "course_preview_viewed",
    expect.objectContaining({
      distinct_id: id,
      source: "client",
      platform: "web",
      app_version: "0.1.0",
      preview_kind: "shared_demo",
      demo_version: "sky-v1",
    }),
  );
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_control_activated",
    expect.objectContaining({
      control_id: "shared_demo",
      surface: "course_brief",
    }),
  );
  expect(JSON.stringify(vi.mocked(posthog.capture).mock.calls)).not.toContain(
    "Reading the sky",
  );
  expect(vi.mocked(posthog.capture).mock.calls.map((call) => call[0])).toEqual([
    "ui_control_activated",
    "course_preview_viewed",
  ]);
  view.unmount();
  vi.clearAllMocks();
  configureAnalytics({ learnerId: id, optedOut: true, platform: "web" });
  render(<SharedDemo surface="welcome" onReturn={() => {}} />);
  await screen.findByRole("heading", { name: "Reading the sky" });
  expect(posthog.capture).not.toHaveBeenCalled();
});
it("retains Return and Retry on failed or empty response", async () => {
  server.use(http.get("*/v1/courses/shared-demo", () => HttpResponse.json({})));
  const back = vi.fn();
  render(<SharedDemo surface="welcome" onReturn={back} />);
  await screen.findByRole("alert");
  expect(
    screen.getByRole("button", { name: "Retry shared example" }),
  ).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Return to welcome" }));
  expect(back).toHaveBeenCalledOnce();
});
it("invalidates a pending paid quote when demo opens and ignores its late resolution", async () => {
  let resolve!: (value: {
    id: string;
    content_ceiling: number;
    pricing_version: string;
    expires_at: string;
  }) => void;
  const service = {
    quote: vi.fn(
      () =>
        new Promise<Parameters<typeof resolve>[0]>((r) => {
          resolve = r;
        }),
    ),
    accept: vi.fn(),
    preview: vi.fn(),
  };
  const props = {
    revision: { id: "brief", content_hash: "h", brief: emptyBrief() },
    service,
    onBack: () => {},
  };
  const view = render(<CoursePreview {...props} demoEpoch={0} />);
  fireEvent.click(screen.getByRole("button", { name: "Review preview cost" }));
  view.rerender(<CoursePreview {...props} demoEpoch={1} />);
  resolve({
    id: "old",
    content_ceiling: 25,
    pricing_version: "stale",
    expires_at: new Date(Date.now() + 300000).toISOString(),
  });
  await waitFor(() =>
    expect(screen.queryByText(/stale/)).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("button", { name: /Confirm & preview/ }),
  ).not.toBeInTheDocument();
  expect(service.accept).not.toHaveBeenCalled();
  expect(service.preview).not.toHaveBeenCalled();
});

it("keeps edited outline content mounted across the demo boundary", async () => {
  const service = {
    quote: vi.fn().mockResolvedValue({
      id: "q",
      content_ceiling: 25,
      pricing_version: "fixture",
      expires_at: new Date(Date.now() + 300000).toISOString(),
    }),
    accept: vi.fn().mockResolvedValue({}),
    preview: vi.fn().mockResolvedValue(fixture.preview),
  };
  const props = {
    revision: { id: "outline-brief", content_hash: "h", brief: emptyBrief() },
    service,
    onBack: () => {},
  };
  const view = render(<CoursePreview {...props} demoEpoch={0} />);
  fireEvent.click(screen.getByRole("button", { name: "Review preview cost" }));
  fireEvent.click(
    await screen.findByRole("button", { name: /Confirm & preview/ }),
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Review sample outline" }),
  );
  const title = await screen.findByLabelText("Title for Earth turns");
  fireEvent.change(title, { target: { value: "My edited lesson" } });
  view.rerender(<CoursePreview {...props} demoEpoch={1} />);
  expect(screen.getByLabelText("Title for My edited lesson")).toHaveValue(
    "My edited lesson",
  );
  expect(service.preview).toHaveBeenCalledOnce();
});

it("keeps unfinished guided answers and composer when returning from demo", async () => {
  server.use(
    http.get("*/v1/courses/shared-demo", () => HttpResponse.json(fixture)),
  );
  const service = {
    config: vi.fn().mockResolvedValue({
      enabled: true,
      credits: 0,
      pricing_version: "unapproved",
    }),
    chat: vi.fn(),
    confirm: vi.fn(),
  };
  render(<CourseBrief learnerId="guided-demo-owner" service={service} />);
  const composer = await screen.findByRole("textbox", { name: "Your answer" });
  fireEvent.change(composer, {
    target: { value: "An unfinished private answer" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Explore shared example" }),
  );
  await screen.findByRole("heading", { name: "Reading the sky" });
  fireEvent.click(
    screen.getByRole("button", { name: "Return to personalized creation" }),
  );
  expect(screen.getByRole("textbox", { name: "Your answer" })).toHaveValue(
    "An unfinished private answer",
  );
  expect(service.chat).not.toHaveBeenCalled();
  expect(service.confirm).not.toHaveBeenCalled();
});
