import { getScheme, applyColorScheme, clearColorScheme } from "./themes";

export interface Settings {
  theme: "dark" | "light" | "auto";
  colorScheme: string;
  showThinking: boolean;
  showToolUse: boolean;
  showToolResult: boolean;
}

const STORAGE_KEY = "agent-recall-settings";

const DEFAULTS: Settings = {
  theme: "auto",
  colorScheme: "default",
  showThinking: true,
  showToolUse: true,
  showToolResult: true,
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

export function applyTheme(theme: Settings["theme"]): void {
  const root = document.documentElement;
  if (theme === "auto") {
    root.removeAttribute("data-theme");
  } else {
    root.setAttribute("data-theme", theme);
  }
}

function getEffectiveMode(theme: Settings["theme"]): "dark" | "light" {
  if (theme === "auto") {
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }
  return theme;
}

export function applyColorSchemeFromSettings(settings: Settings): void {
  if (settings.colorScheme === "default") {
    clearColorScheme();
    return;
  }
  const scheme = getScheme(settings.colorScheme);
  const mode = getEffectiveMode(settings.theme);
  applyColorScheme(scheme, mode);
}
