import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { CharacterExportConfirmation } from "./CharacterExportConfirmation";

it("requires disclosure confirmation and preserves retry without exporting offline", () => {
  const confirm = vi.fn();
  const { rerender } = render(
    <CharacterExportConfirmation
      names={["A very long original guide name"]}
      online={true}
      busy={false}
      onConfirm={confirm}
    />,
  );
  expect(
    screen.getByText(
      /name, bio, backstory, speaking style, persona and appearance/,
    ),
  ).toBeVisible();
  const button = screen.getByRole("button", {
    name: "Confirm Character export",
  });
  expect(button).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox"));
  rerender(
    <CharacterExportConfirmation
      names={["A very long original guide name"]}
      online={false}
      busy={false}
      onConfirm={confirm}
    />,
  );
  expect(button).toBeDisabled();
  expect(confirm).not.toHaveBeenCalled();
  rerender(
    <CharacterExportConfirmation
      names={["A very long original guide name"]}
      online={true}
      busy={false}
      onConfirm={confirm}
    />,
  );
  fireEvent.click(button);
  expect(confirm).toHaveBeenCalledTimes(1);
});
