import { isChatEdgeEnabled } from "../../lib/features";
import type * as ChatTransport from "./chat-transport";

type EdgeChat = Pick<
  typeof ChatTransport,
  "edgeTutorService" | "sendGuidedTurn"
>;

export function withChatTransport<T>(
  runEdge: (transport: EdgeChat) => Promise<T>,
  runLegacy: () => Promise<T>,
): Promise<T> {
  if (!isChatEdgeEnabled()) return runLegacy();

  // The Go path is also available if the browser cannot load the edge chunk.
  return import("./chat-transport").then(
    ({ edgeTutorService, sendGuidedTurn }) =>
      runEdge({ edgeTutorService, sendGuidedTurn }),
    runLegacy,
  );
}
