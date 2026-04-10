export interface Settings {
  theme: "dark" | "light" | "auto";
  showThinking: boolean;
  showToolUse: boolean;
  showToolResult: boolean;
}

const STORAGE_KEY = "agent-recall-settings";

const DEFAULTS: Settings = {
  theme: "auto",
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
