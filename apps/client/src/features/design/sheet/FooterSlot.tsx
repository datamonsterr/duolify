import { createContext, useContext } from "react";

const Slot = createContext<HTMLElement | null>(null);

/**
 * A dialog that owns the one bottom footer provides its slot here; guided chats inside it
 * render their action buttons into that footer instead of mid-dialog, and drop their own Cancel.
 */
export const FooterSlotProvider = Slot.Provider;
export const useFooterSlot = () => useContext(Slot);

/** Portal target inside an ActionFooter, after its secondary buttons (primary stays right-most). */
export function FooterSlot({
  onSlot,
}: {
  onSlot: (node: HTMLElement | null) => void;
}) {
  return <span ref={onSlot} className="action-footer-slot" />;
}
