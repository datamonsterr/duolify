import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { InstructionPanel } from "./InstructionPanel";

describe("private course steering", () => {
  it("keeps rejected drafts, retries screening and shows saved state without text analytics", async () => {
    const service = {
      latest: vi.fn().mockResolvedValue(null),
      add: vi
        .fn()
        .mockRejectedValueOnce(new Error("screening_unavailable"))
        .mockResolvedValue({
          id: "i1",
          text: "More examples in Go",
          scope: "next_lesson",
          status: "active",
        }),
    };
    render(<InstructionPanel courseId="c1" service={service} />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading instruction");
    await screen.findByText("No instruction yet.");
    fireEvent.change(screen.getByLabelText("New instruction"), {
      target: { value: "More examples in Go" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save instruction" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText("New instruction")).toHaveValue(
      "More examples in Go",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry saving" }));
    await screen.findByText("Active");
    expect(service.add).toHaveBeenLastCalledWith("c1", {
      text: "More examples in Go",
      scope: "next_lesson",
    });
  });
  it("recovers loading failures and preserves long applied content", async () => {
    const service = {
      latest: vi
        .fn()
        .mockRejectedValueOnce(new Error("offline"))
        .mockResolvedValue({
          id: "i1",
          text: "語".repeat(500),
          scope: "next_lesson",
          status: "applied",
          applied_lesson_id: "l1",
        }),
      add: vi.fn(),
    };
    render(<InstructionPanel courseId="c1" service={service} />);
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await screen.findByText("Applied to lesson l1");
    expect(screen.getByText("語".repeat(500))).toBeVisible();
    await waitFor(() => expect(service.latest).toHaveBeenCalledTimes(2));
  });
  it("counts Unicode characters, blocks oversized drafts and sends the selected scope", async () => {
    const service = {
      latest: vi.fn().mockResolvedValue(null),
      add: vi.fn().mockResolvedValue({
        id: "i2",
        text: "語".repeat(500),
        scope: "rest_of_course",
        status: "active",
      }),
    };
    render(<InstructionPanel courseId="c1" service={service} />);
    await screen.findByText("No instruction yet.");
    fireEvent.change(screen.getByLabelText("New instruction"), {
      target: { value: "語".repeat(501) },
    });
    expect(
      screen.getByRole("button", { name: "Save instruction" }),
    ).toBeDisabled();
    fireEvent.change(screen.getByLabelText("New instruction"), {
      target: { value: "語".repeat(500) },
    });
    expect(screen.getByText(/500 \/ 500/)).toBeVisible();
    fireEvent.click(screen.getByRole("radio", { name: "Rest of course" }));
    fireEvent.click(screen.getByRole("button", { name: "Save instruction" }));
    await screen.findByText("Active");
    expect(service.add).toHaveBeenCalledWith("c1", {
      text: "語".repeat(500),
      scope: "rest_of_course",
    });
  });
});
