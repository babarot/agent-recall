import { getScheme, applyColorScheme, clearColorScheme } from "./themes";

export interface Settings {
  theme: "dark" | "light" | "auto";
  colorScheme: string;
  showThinking: boolean;
  showToolUse: boolean;
  showToolResult: boolean;
  showMeta: boolean;
  startAtBottom: boolean;
}

const STORAGE_KEY = "agent-recall-settings";

const DEFAULTS: Settings = {
  theme: "auto",
  colorScheme: "default",
  showThinking: true,
  showToolUse: true,
  showToolResult: true,
  showMeta: true,
  startAtBottom: false,
};

export function loadSettings(): Settings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULTS };
    return { ...DEFAULTS, ...JSON.parse(raw) };
  } catch {
    return { ...DEFAULTS };
  }
}

export function saveSettings(settings: Settings): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(settings));
}

export function getEffectiveMode(theme: Settings["theme"]): "dark" | "light" {
  if (theme === "auto") {
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }
  return theme;
}

/** Apply all visual settings (theme mode + color scheme) in one call */
export function applyAllSettings(settings: Settings): void {
  // Theme mode (data-theme attribute)
  const root = document.documentElement;
  if (settings.theme === "auto") {
    root.removeAttribute("data-theme");
  } else {
    root.setAttribute("data-theme", settings.theme);
  }

  // Color scheme (CSS variables)
  if (settings.colorScheme === "default") {
    clearColorScheme();
  } else {
    const scheme = getScheme(settings.colorScheme);
    const mode = getEffectiveMode(settings.theme);
    applyColorScheme(scheme, mode);
  }
}
