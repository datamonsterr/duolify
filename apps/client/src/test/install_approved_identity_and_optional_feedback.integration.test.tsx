import fs from "node:fs";
import path from "node:path";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "./setup";
import {
  Brand,
  BrandMark,
  BrandWordmark,
} from "../features/design/brand/Brand";
import {
  triggerFeedbackCue,
  setFeedbackSettings,
} from "../features/design/feedback/feedbackBridge";
import { Button } from "../features/design/Button";
import { LessonTest } from "../features/lessontest/LessonTest";
import { Settings } from "../features/accounts/Settings";
import { emptySettings } from "../features/accounts/learner-context";
import { credentialStore, session } from "../features/accounts/session";
import { configureUiAnalytics } from "../lib/analytics";

const telemetry = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));

vi.mock("posthog-js/dist/module.no-external", () => ({ default: telemetry }));

describe("install_approved_identity_and_optional_feedback integration", () => {
  let vibrateSpy: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    vi.clearAllMocks();
    vibrateSpy = vi.fn();
    Object.defineProperty(navigator, "vibrate", {
      value: vibrateSpy,
      configurable: true,
      writable: true,
    });
    await credentialStore.write({
      access_token: "test-access",
      refresh_token: "test-refresh",
      expires_in: 900,
    });
    server.use(
      http.get("*/v1/me", () =>
        HttpResponse.json({
          id: "10000000-0000-4000-8000-000000000001",
          display_name: "Learner",
          locale: "en",
          time_zone: "UTC",
          analytics_opt_out: false,
        }),
      ),
      http.get("*/v1/social/identity", () =>
        HttpResponse.json({
          username: "learner",
          display_name: "Learner",
        }),
      ),
      http.get("*/v1/social/settings", () =>
        HttpResponse.json({
          course_invites_from: "friends",
          findable_by_username: true,
        }),
      ),
    );
    configureUiAnalytics({
      learnerId: "10000000-0000-4000-8000-000000000001",
      enabled: true,
      platform: "web",
      appVersion: "0.1.0",
    });
  });

  afterEach(async () => {
    await credentialStore.write(null);
    session.clear();
    configureUiAnalytics(null);
  });

  describe("Brand components and vector geometry", () => {
    it("renders the approved BrandMark with two offset tiles and accessible attributes", () => {
      const { container } = render(<BrandMark size={32} />);
      const mark = container.querySelector('[data-brand-mark="true"]');
      expect(mark).toBeInTheDocument();
      expect(mark).toHaveAttribute("aria-hidden", "true");

      const tiles = container.querySelectorAll("[data-tile]");
      expect(tiles.length).toBe(2);

      // Tile 1: lime (#58C322)
      expect(tiles[0]).toHaveAttribute("data-tile", "lime");
      // Tile 2: secondary (#1677C9)
      expect(tiles[1]).toHaveAttribute("data-tile", "secondary");
    });

    it("renders standalone BrandMark with accessible label when decorative is false", () => {
      render(<BrandMark decorative={false} label="Duolify Home" />);
      expect(screen.getByLabelText("Duolify Home")).toBeInTheDocument();
    });

    it("renders BrandWordmark with lowercase text and token font", () => {
      render(<BrandWordmark />);
      const wordmark = screen.getByText("duolify");
      expect(wordmark).toBeInTheDocument();
      expect(wordmark).toHaveAttribute("data-brand-wordmark", "true");
    });

    it("renders composite Brand component linking to root", () => {
      render(<Brand href="/" />);
      const brandLink = screen.getByRole("link", { name: /duolify/i });
      expect(brandLink).toHaveAttribute("href", "/");
      expect(
        brandLink.querySelector('[data-brand-mark="true"]'),
      ).toBeInTheDocument();
    });

    it("verifies PNG header dimensions of manifest and Tauri icon assets match declared sizes", () => {
      function getPngDimensions(relPath: string) {
        const fullPath = path.resolve(__dirname, relPath);
        const buffer = fs.readFileSync(fullPath);
        expect(buffer.toString("ascii", 1, 4)).toBe("PNG");
        return {
          width: buffer.readUInt32BE(16),
          height: buffer.readUInt32BE(20),
        };
      }

      // Web manifest icons
      expect(getPngDimensions("../../public/icon-192.png")).toEqual({
        width: 192,
        height: 192,
      });
      expect(getPngDimensions("../../public/icon-512.png")).toEqual({
        width: 512,
        height: 512,
      });

      // Tauri desktop and mobile standard sizes
      expect(getPngDimensions("../../src-tauri/icons/32x32.png")).toEqual({
        width: 32,
        height: 32,
      });
      expect(getPngDimensions("../../src-tauri/icons/128x128.png")).toEqual({
        width: 128,
        height: 128,
      });
      expect(getPngDimensions("../../src-tauri/icons/icon.png")).toEqual({
        width: 512,
        height: 512,
      });
    });
  });

  describe("Tauri sound/haptic platform bridge double", () => {
    it("triggers silent no-op sound cue without audio elements or network fetches", async () => {
      const fetchSpy = vi.spyOn(globalThis, "fetch");
      const audioPlaySpy = vi.fn();
      window.Audio = vi.fn().mockImplementation(() => ({
        play: audioPlaySpy,
      })) as unknown as typeof Audio;

      triggerFeedbackCue({
        type: "tap",
        soundEnabled: true,
        hapticsSetting: "system",
        reduceMotion: false,
      });

      expect(fetchSpy).not.toHaveBeenCalled();
      expect(audioPlaySpy).not.toHaveBeenCalled();
      fetchSpy.mockRestore();
    });

    it("invokes haptics via platform bridge when enabled and not reduced motion", () => {
      triggerFeedbackCue({
        type: "completion",
        soundEnabled: false,
        hapticsSetting: "on",
        reduceMotion: false,
      });

      expect(vibrateSpy).toHaveBeenCalledWith([20, 40, 20]);
    });

    it("keeps paired pulse haptics on completion even when reduced motion is requested", () => {
      triggerFeedbackCue({
        type: "completion",
        soundEnabled: false,
        hapticsSetting: "on",
        reduceMotion: true,
      });

      // Reduced motion suppresses celebration movement only, not opt-in haptics
      expect(vibrateSpy).toHaveBeenCalledWith([20, 40, 20]);
    });

    it("never produces punishment vibration on mistakes or corrections", () => {
      triggerFeedbackCue({
        type: "correction",
        soundEnabled: true,
        hapticsSetting: "on",
        reduceMotion: false,
      });

      // Corrections must be neutral/silent, never punishing
      expect(vibrateSpy).not.toHaveBeenCalled();
    });

    it("wires tap feedback into shared design Button confirmed press", () => {
      setFeedbackSettings({ haptics: "on" });
      const onClick = vi.fn();
      render(<Button onClick={onClick}>Confirm action</Button>);

      fireEvent.click(screen.getByRole("button", { name: "Confirm action" }));
      expect(onClick).toHaveBeenCalled();
      expect(vibrateSpy).toHaveBeenCalledWith([15]);
    });

    it("wires correction feedback into lesson-test wrong answer without vibration", async () => {
      setFeedbackSettings({ haptics: "on" });
      const onAnswer = vi.fn().mockResolvedValue({
        correct: false,
        explanation: "Incorrect answer explanation",
      });

      render(
        <LessonTest
          lessonId="l1"
          courseId="c1"
          title="Sample Test"
          questions={[
            {
              question_id: "q1",
              topic_id: "t1",
              prompt: "Select correct choice",
              choices: ["Choice A", "Choice B"],
            },
          ]}
          onPass={vi.fn()}
          onExit={vi.fn()}
          onAnswer={onAnswer}
        />,
      );

      fireEvent.click(screen.getByLabelText("Choice A"));
      fireEvent.click(screen.getByRole("button", { name: /Check/i }));

      await screen.findByText(/Not quite/i);
      expect(onAnswer).toHaveBeenCalled();
      // Correction cue was called, but must be a no-op haptic
      expect(vibrateSpy).not.toHaveBeenCalled();
    });
  });

  describe("Settings feedback preferences persistence and analytics", () => {
    it("persists feedback settings and emits ui_control_activated", async () => {
      const savedProfile = {
        ...emptySettings(),
        learner_id: "10000000-0000-4000-8000-000000000001",
        ui_sounds: false,
        haptics: "system" as const,
      };

      let patchReceived: Record<string, unknown> | null = null;

      server.use(
        http.get("*/v1/me/settings", () => HttpResponse.json(savedProfile)),
        http.post("*/v1/me/settings", async ({ request }) => {
          const body = (await request.json()) as {
            fields: Record<string, unknown>;
          };
          patchReceived = body.fields;
          return HttpResponse.json({ ...savedProfile, ...body.fields });
        }),
      );

      render(<Settings />);

      await screen.findByRole("heading", { name: "Settings" });

      const soundsCheckbox = await screen.findByLabelText("UI sounds", {
        exact: true,
      });
      expect(soundsCheckbox).not.toBeChecked();

      const hapticsSelect = screen.getByLabelText("Haptics", { exact: true });
      expect(hapticsSelect).toHaveValue("system");

      fireEvent.click(soundsCheckbox);
      expect(soundsCheckbox).toBeChecked();

      fireEvent.change(hapticsSelect, { target: { value: "on" } });

      fireEvent.click(screen.getByRole("button", { name: "Save settings" }));

      await waitFor(() => {
        expect(patchReceived).toEqual(
          expect.objectContaining({
            ui_sounds: true,
            haptics: "on",
          }),
        );
      });

      expect(await screen.findByText("Settings saved")).toBeInTheDocument();

      // Check analytics events
      expect(telemetry.capture).toHaveBeenCalledWith(
        "ui_control_activated",
        expect.objectContaining({
          surface: "settings",
          control_id: "ui_sounds",
        }),
      );
      expect(telemetry.capture).toHaveBeenCalledWith(
        "ui_control_activated",
        expect.objectContaining({
          surface: "settings",
          control_id: "haptics",
        }),
      );
    });

    it("displays error feedback and preserves draft on save failure", async () => {
      const savedProfile = {
        ...emptySettings(),
        learner_id: "10000000-0000-4000-8000-000000000001",
        ui_sounds: false,
        haptics: "system" as const,
      };

      server.use(
        http.get("*/v1/me/settings", () => HttpResponse.json(savedProfile)),
        http.post("*/v1/me/settings", () =>
          HttpResponse.json(
            { error: "Could not save settings. Retry." },
            { status: 500 },
          ),
        ),
      );

      render(<Settings />);
      await screen.findByRole("heading", { name: "Settings" });

      const soundsCheckbox = await screen.findByLabelText("UI sounds", {
        exact: true,
      });
      fireEvent.click(soundsCheckbox);
      expect(soundsCheckbox).toBeChecked();

      fireEvent.click(screen.getByRole("button", { name: "Save settings" }));

      await waitFor(() => {
        expect(
          screen.getByText(/Could not save settings/i),
        ).toBeInTheDocument();
      });

      // Draft must be preserved on error
      expect(soundsCheckbox).toBeChecked();
    });
  });
});
