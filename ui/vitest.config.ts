import { defineConfig } from "vitest/config";
import preact from "@preact/preset-vite";

export default defineConfig({
  plugins: [preact()],
  test: {
    // happy-dom gives us `document`, `window`, and friends for hook tests
    // without pulling in a full jsdom. Pure utility tests (settings,
    // chat-utils) still run fine under this environment.
    environment: "happy-dom",
  },
});
