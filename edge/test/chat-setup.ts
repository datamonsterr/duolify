import { afterAll, afterEach, beforeAll } from "vitest";
import { network } from "./chat-network";

beforeAll(() => network.enable());
afterEach(() => network.resetHandlers());
afterAll(() => network.disable());
