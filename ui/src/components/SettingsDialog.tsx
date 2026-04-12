import { Monitor, Moon, Sun, Palette, SunMoon, MessageSquare, List } from "lucide-preact";
import type { Settings } from "../lib/settings";
import { COLOR_SCHEMES } from "../lib/themes";

interface Props {
  settings: Settings;
  onChange: (settings: Settings) => void;
  onClose: () => void;
}

export function SettingsDialog({ settings, onChange, onClose }: Props) {
  const update = (patch: Partial<Settings>) => {
    onChange({ ...settings, ...patch });
  };

  const isDark = settings.theme === "dark" ||
    (settings.theme === "auto" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  return (
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div class="bg-bg-secondary border border-border rounded-xl w-full max-w-lg p-6 shadow-xl max-h-[85vh] overflow-y-auto">
        <div class="flex items-center justify-between mb-6">
          <h2 class="text-lg font-semibold text-text">Settings</h2>
          <button
            onClick={onClose}
            class="text-text-muted hover:text-text transition-colors cursor-pointer text-lg"
          >
            &times;
          </button>
        </div>

        {/* Theme */}
        <section class="mb-6">
          <h3 class="text-base font-semibold text-text mb-3 flex items-center gap-2">
            <SunMoon size={16} /> Theme
          </h3>
          <div class="flex gap-2">
            {([
              { key: "auto", label: "Auto", icon: <Monitor size={16} /> },
              { key: "dark", label: "Dark", icon: <Moon size={16} /> },
              { key: "light", label: "Light", icon: <Sun size={16} /> },
            ] as const).map(({ key, label, icon }) => (
              <button
                key={key}
                onClick={() => update({ theme: key })}
                class={`flex items-center gap-2 px-5 py-2.5 text-sm rounded-lg border cursor-pointer transition-colors ${
                  settings.theme === key
                    ? "bg-accent text-white border-accent"
                    : "bg-bg border-border text-text-secondary hover:text-text"
                }`}
              >
                {icon} {label}
              </button>
            ))}
          </div>
        </section>

        {/* Color Scheme */}
        <section class="mb-6">
          <h3 class="text-base font-semibold text-text mb-3 flex items-center gap-2">
            <Palette size={16} /> Color Scheme
          </h3>
          <div class="grid grid-cols-4 gap-2">
            {COLOR_SCHEMES.map((scheme) => {
              const colors = isDark ? scheme.dark : scheme.light;
              const isActive = settings.colorScheme === scheme.name;
              return (
                <button
                  key={scheme.name}
                  onClick={() => update({ colorScheme: scheme.name })}
                  class={`rounded-lg border p-2 cursor-pointer transition-all ${
                    isActive
                      ? "border-accent ring-1 ring-accent"
                      : "border-border hover:border-text-muted"
                  }`}
                >
                  {/* Color preview */}
                  <div
                    class="rounded-md h-8 mb-1.5 flex items-center gap-0.5 px-1.5 overflow-hidden"
                    style={{ background: colors.bg }}
                  >
                    <div class="w-2 h-2 rounded-full" style={{ background: colors.accent }} />
                    <div class="w-2 h-2 rounded-full" style={{ background: colors.text }} />
                    <div class="w-2 h-2 rounded-full" style={{ background: colors.textMuted }} />
                    <div class="flex-1 h-1 rounded ml-0.5" style={{ background: colors.bgTertiary }} />
                  </div>
                  <div class="text-xs text-center truncate text-text-secondary">{scheme.label}</div>
                </button>
              );
            })}
          </div>
        </section>

        {/* Chat display */}
        <section>
          <h3 class="text-base font-semibold text-text mb-3 flex items-center gap-2">
            <MessageSquare size={16} /> Chat Display
          </h3>
          <div class="space-y-4">
            <Toggle
              label="Show thinking"
              description="Display assistant's reasoning process"
              checked={settings.showThinking}
              onChange={(v) => update({ showThinking: v })}
            />
            <Toggle
              label="Show tool calls"
              description="Display Bash, Read, Write, WebFetch, etc."
              checked={settings.showToolUse}
              onChange={(v) => update({ showToolUse: v })}
            />
            <Toggle
              label="Show tool results"
              description="Display output from tool executions"
              checked={settings.showToolResult}
              onChange={(v) => update({ showToolResult: v })}
            />
            <Toggle
              label="Show meta messages"
              description="Display synthetic content injected by Claude Code (skill expansions, context blocks, bash caveats)"
              checked={settings.showMeta}
              onChange={(v) => update({ showMeta: v })}
            />
          </div>
        </section>

        {/* Session List */}
        <section class="mt-6">
          <h3 class="text-base font-semibold text-text mb-3 flex items-center gap-2">
            <List size={16} /> Session List
          </h3>
          <div class="space-y-4">
            <Toggle
              label="Start at bottom"
              description="Open sessions scrolled to the latest message instead of the first"
              checked={settings.startAtBottom}
              onChange={(v) => update({ startAtBottom: v })}
            />
            <Toggle
              label="Show sparklines"
              description="Display activity sparkline charts in session cards"
              checked={settings.showSparkline}
              onChange={(v) => update({ showSparkline: v })}
            />
          </div>
        </section>
      </div>
    </div>
  );
}

function Toggle({ label, description, checked, onChange }: {
  label: string;
  description: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <label class="flex items-start gap-4 cursor-pointer">
      {/* flex-1 min-w-0 lets the description wrap inside the available
          width instead of pushing the toggle out of the row. */}
      <div class="flex-1 min-w-0">
        <div class="text-sm text-text">{label}</div>
        <div class="text-xs text-text-muted">{description}</div>
      </div>
      <button
        onClick={() => onChange(!checked)}
        class={`relative w-10 h-6 rounded-full transition-colors cursor-pointer shrink-0 mt-0.5 ${
          checked ? "bg-accent" : "bg-border"
        }`}
      >
        <span
          class={`absolute top-1 left-1 w-4 h-4 rounded-full bg-white transition-transform ${
            checked ? "translate-x-4" : ""
          }`}
        />
      </button>
    </label>
  );
}
