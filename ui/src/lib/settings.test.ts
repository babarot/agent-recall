import { describe, it, expect, beforeEach } from "vitest";
import { loadSettings, saveSettings } from "./settings";
import type { Settings } from "./settings";

// Mock localStorage
const store: Record<string, string> = {};
const localStorageMock = {
  getItem: (key: string) => store[key] ?? null,
  setItem: (key: string, value: string) => { store[key] = value; },
  removeItem: (key: string) => { delete store[key]; },
  clear: () => { for (const key in store) delete store[key]; },
  get length() { return Object.keys(store).length; },
  key: (i: number) => Object.keys(store)[i] ?? null,
};
Object.defineProperty(globalThis, "localStorage", { value: localStorageMock });

beforeEach(() => {
  localStorageMock.clear();
});

describe("loadSettings", () => {
  it("returns defaults when nothing is saved", () => {
    const s = loadSettings();
    expect(s.theme).toBe("auto");
    expect(s.colorScheme).toBe("default");
    expect(s.showThinking).toBe(true);
    expect(s.showToolUse).toBe(true);
    expect(s.showToolResult).toBe(true);
    expect(s.showMeta).toBe(true);
    expect(s.startAtBottom).toBe(false);
    expect(s.showSparkline).toBe(true);
  });

  it("loads saved settings", () => {
    localStorageMock.setItem("agent-recall-settings", JSON.stringify({
      theme: "dark",
      showThinking: false,
    }));
    const s = loadSettings();
    expect(s.theme).toBe("dark");
    expect(s.showThinking).toBe(false);
    // Defaults for missing keys
    expect(s.showToolUse).toBe(true);
    expect(s.showToolResult).toBe(true);
    expect(s.showMeta).toBe(true);
  });

  it("backfills showMeta when it is missing from an older saved shape", () => {
    // Users who have existing saved settings from before showMeta existed
    // should get the default (true) without losing their other prefs.
    localStorageMock.setItem("agent-recall-settings", JSON.stringify({
      theme: "dark",
      showThinking: false,
      showToolUse: false,
      showToolResult: true,
    }));
    const s = loadSettings();
    expect(s.showMeta).toBe(true);
    expect(s.showThinking).toBe(false);
    expect(s.showToolUse).toBe(false);
  });

  it("honors an explicitly disabled showMeta", () => {
    localStorageMock.setItem("agent-recall-settings", JSON.stringify({
      showMeta: false,
    }));
    const s = loadSettings();
    expect(s.showMeta).toBe(false);
  });

  it("backfills startAtBottom when it is missing from an older saved shape", () => {
    // Users with saved settings from before startAtBottom existed should
    // get the default (false) without losing their other prefs.
    localStorageMock.setItem("agent-recall-settings", JSON.stringify({
      theme: "dark",
      showThinking: false,
    }));
    const s = loadSettings();
    expect(s.startAtBottom).toBe(false);
    expect(s.theme).toBe("dark");
    expect(s.showThinking).toBe(false);
  });

  it("honors an explicitly enabled startAtBottom", () => {
    localStorageMock.setItem("agent-recall-settings", JSON.stringify({
      startAtBottom: true,
    }));
    const s = loadSettings();
    expect(s.startAtBottom).toBe(true);
  });

  it("handles corrupted JSON gracefully", () => {
    localStorageMock.setItem("agent-recall-settings", "not json");
    const s = loadSettings();
    expect(s.theme).toBe("auto");
  });
});

describe("saveSettings", () => {
  it("persists settings to localStorage", () => {
    const settings: Settings = {
      theme: "light",
      colorScheme: "tokyo-night",
      showThinking: false,
      showToolUse: true,
      showToolResult: false,
      showMeta: true,
      startAtBottom: true,
      showSparkline: false,
    };
    saveSettings(settings);
    const raw = localStorageMock.getItem("agent-recall-settings");
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw!);
    expect(parsed.theme).toBe("light");
    expect(parsed.showThinking).toBe(false);
    expect(parsed.showToolResult).toBe(false);
    expect(parsed.showMeta).toBe(true);
    expect(parsed.startAtBottom).toBe(true);
    expect(parsed.showSparkline).toBe(false);
  });

  it("roundtrips through load", () => {
    const settings: Settings = {
      theme: "dark",
      colorScheme: "nord",
      showThinking: false,
      showToolUse: false,
      showToolResult: true,
      showMeta: false,
      startAtBottom: true,
      showSparkline: true,
    };
    saveSettings(settings);
    const loaded = loadSettings();
    expect(loaded).toEqual(settings);
  });
});
