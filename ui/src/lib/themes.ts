export interface ThemeColors {
  bg: string;
  bgSecondary: string;
  bgTertiary: string;
  border: string;
  text: string;
  textSecondary: string;
  textMuted: string;
  accent: string;
  accentHover: string;
  userBubble: string;
  assistantBubble: string;
  success: string;
  codeBg: string;
}

export interface ColorScheme {
  name: string;
  label: string;
  dark: ThemeColors;
  light: ThemeColors;
}

export const COLOR_SCHEMES: ColorScheme[] = [
  {
    name: "default",
    label: "Default",
    dark: {
      bg: "#0d1117", bgSecondary: "#161b22", bgTertiary: "#1c2128",
      border: "#30363d", text: "#e6edf3", textSecondary: "#8b949e", textMuted: "#656d76",
      accent: "#58a6ff", accentHover: "#79c0ff",
      userBubble: "#1c2f50", assistantBubble: "#21262d",
      success: "#3fb950", codeBg: "#161b22",
    },
    light: {
      bg: "#ffffff", bgSecondary: "#f6f8fa", bgTertiary: "#eef1f5",
      border: "#d0d7de", text: "#1f2328", textSecondary: "#656d76", textMuted: "#8b949e",
      accent: "#0969da", accentHover: "#0550ae",
      userBubble: "#ddf4ff", assistantBubble: "#f6f8fa",
      success: "#1a7f37", codeBg: "#f6f8fa",
    },
  },
  {
    name: "tokyo-night",
    label: "Tokyo Night",
    dark: {
      bg: "#1a1b26", bgSecondary: "#16161e", bgTertiary: "#24283b",
      border: "#2f3348", text: "#a9b1d6", textSecondary: "#565f89", textMuted: "#444b6a",
      accent: "#7aa2f7", accentHover: "#89b4fa",
      userBubble: "#283457", assistantBubble: "#1e2030",
      success: "#9ece6a", codeBg: "#16161e",
    },
    light: {
      bg: "#d5d6db", bgSecondary: "#c8c9ce", bgTertiary: "#cbccd1",
      border: "#b4b5ba", text: "#343b58", textSecondary: "#6e7191", textMuted: "#9699a3",
      accent: "#34548a", accentHover: "#2a4375",
      userBubble: "#b4c1dc", assistantBubble: "#c8c9ce",
      success: "#485e30", codeBg: "#c8c9ce",
    },
  },
  {
    name: "nord",
    label: "Nord",
    dark: {
      bg: "#2e3440", bgSecondary: "#3b4252", bgTertiary: "#434c5e",
      border: "#4c566a", text: "#eceff4", textSecondary: "#d8dee9", textMuted: "#81a1c1",
      accent: "#88c0d0", accentHover: "#8fbcbb",
      userBubble: "#3b4f65", assistantBubble: "#3b4252",
      success: "#a3be8c", codeBg: "#3b4252",
    },
    light: {
      bg: "#eceff4", bgSecondary: "#e5e9f0", bgTertiary: "#d8dee9",
      border: "#c1c8d6", text: "#2e3440", textSecondary: "#4c566a", textMuted: "#7b88a1",
      accent: "#5e81ac", accentHover: "#4c6e96",
      userBubble: "#d1dce8", assistantBubble: "#e5e9f0",
      success: "#6d8a5e", codeBg: "#e5e9f0",
    },
  },
  {
    name: "solarized",
    label: "Solarized",
    dark: {
      bg: "#002b36", bgSecondary: "#073642", bgTertiary: "#0a4050",
      border: "#586e75", text: "#839496", textSecondary: "#657b83", textMuted: "#586e75",
      accent: "#268bd2", accentHover: "#2aa198",
      userBubble: "#0d3d4d", assistantBubble: "#073642",
      success: "#859900", codeBg: "#073642",
    },
    light: {
      bg: "#fdf6e3", bgSecondary: "#eee8d5", bgTertiary: "#e8e1ca",
      border: "#d3cbb7", text: "#657b83", textSecondary: "#93a1a1", textMuted: "#b0b8b8",
      accent: "#268bd2", accentHover: "#2aa198",
      userBubble: "#e6e0ce", assistantBubble: "#eee8d5",
      success: "#859900", codeBg: "#eee8d5",
    },
  },
  {
    name: "dracula",
    label: "Dracula",
    dark: {
      bg: "#282a36", bgSecondary: "#21222c", bgTertiary: "#343746",
      border: "#44475a", text: "#f8f8f2", textSecondary: "#bfbfbf", textMuted: "#6272a4",
      accent: "#bd93f9", accentHover: "#caa9fa",
      userBubble: "#3d2f5c", assistantBubble: "#21222c",
      success: "#50fa7b", codeBg: "#21222c",
    },
    light: {
      bg: "#f8f8f2", bgSecondary: "#f0f0e8", bgTertiary: "#e8e8e0",
      border: "#d0d0c8", text: "#282a36", textSecondary: "#6272a4", textMuted: "#9ea4b8",
      accent: "#7c3aed", accentHover: "#6d28d9",
      userBubble: "#e8dff5", assistantBubble: "#f0f0e8",
      success: "#2e8b57", codeBg: "#f0f0e8",
    },
  },
  {
    name: "catppuccin",
    label: "Catppuccin",
    dark: {
      bg: "#1e1e2e", bgSecondary: "#181825", bgTertiary: "#313244",
      border: "#45475a", text: "#cdd6f4", textSecondary: "#a6adc8", textMuted: "#6c7086",
      accent: "#89b4fa", accentHover: "#74c7ec",
      userBubble: "#2a3150", assistantBubble: "#181825",
      success: "#a6e3a1", codeBg: "#181825",
    },
    light: {
      bg: "#eff1f5", bgSecondary: "#e6e9ef", bgTertiary: "#dce0e8",
      border: "#ccd0da", text: "#4c4f69", textSecondary: "#6c6f85", textMuted: "#9ca0b0",
      accent: "#1e66f5", accentHover: "#0550ae",
      userBubble: "#d5dff5", assistantBubble: "#e6e9ef",
      success: "#40a02b", codeBg: "#e6e9ef",
    },
  },
  {
    name: "rose-pine",
    label: "Rosé Pine",
    dark: {
      bg: "#191724", bgSecondary: "#1f1d2e", bgTertiary: "#26233a",
      border: "#403d52", text: "#e0def4", textSecondary: "#908caa", textMuted: "#6e6a86",
      accent: "#c4a7e7", accentHover: "#ebbcba",
      userBubble: "#2e2844", assistantBubble: "#1f1d2e",
      success: "#31748f", codeBg: "#1f1d2e",
    },
    light: {
      bg: "#faf4ed", bgSecondary: "#f2e9e1", bgTertiary: "#ebe5dd",
      border: "#dfdad6", text: "#575279", textSecondary: "#797593", textMuted: "#9893a5",
      accent: "#907aa9", accentHover: "#d7827e",
      userBubble: "#e8ddd5", assistantBubble: "#f2e9e1",
      success: "#286983", codeBg: "#f2e9e1",
    },
  },
  {
    name: "gruvbox",
    label: "Gruvbox",
    dark: {
      bg: "#282828", bgSecondary: "#1d2021", bgTertiary: "#3c3836",
      border: "#504945", text: "#ebdbb2", textSecondary: "#bdae93", textMuted: "#665c54",
      accent: "#83a598", accentHover: "#8ec07c",
      userBubble: "#2e3b35", assistantBubble: "#1d2021",
      success: "#b8bb26", codeBg: "#1d2021",
    },
    light: {
      bg: "#fbf1c7", bgSecondary: "#f2e5bc", bgTertiary: "#ebdbb2",
      border: "#d5c4a1", text: "#3c3836", textSecondary: "#665c54", textMuted: "#928374",
      accent: "#458588", accentHover: "#689d6a",
      userBubble: "#e2d8b0", assistantBubble: "#f2e5bc",
      success: "#79740e", codeBg: "#f2e5bc",
    },
  },
];

export function getScheme(name: string): ColorScheme {
  return COLOR_SCHEMES.find((s) => s.name === name) ?? COLOR_SCHEMES[0];
}

export function applyColorScheme(scheme: ColorScheme, mode: "dark" | "light"): void {
  const colors = scheme[mode];
  const root = document.documentElement;
  root.style.setProperty("--ui-bg", colors.bg);
  root.style.setProperty("--ui-bg-secondary", colors.bgSecondary);
  root.style.setProperty("--ui-bg-tertiary", colors.bgTertiary);
  root.style.setProperty("--ui-border", colors.border);
  root.style.setProperty("--ui-text", colors.text);
  root.style.setProperty("--ui-text-secondary", colors.textSecondary);
  root.style.setProperty("--ui-text-muted", colors.textMuted);
  root.style.setProperty("--ui-accent", colors.accent);
  root.style.setProperty("--ui-accent-hover", colors.accentHover);
  root.style.setProperty("--ui-user-bubble", colors.userBubble);
  root.style.setProperty("--ui-assistant-bubble", colors.assistantBubble);
  root.style.setProperty("--ui-success", colors.success);
  root.style.setProperty("--ui-code-bg", colors.codeBg);
}

export function clearColorScheme(): void {
  const root = document.documentElement;
  const vars = [
    "--ui-bg", "--ui-bg-secondary", "--ui-bg-tertiary", "--ui-border",
    "--ui-text", "--ui-text-secondary", "--ui-text-muted",
    "--ui-accent", "--ui-accent-hover",
    "--ui-user-bubble", "--ui-assistant-bubble", "--ui-success", "--ui-code-bg",
  ];
  for (const v of vars) {
    root.style.removeProperty(v);
  }
}
