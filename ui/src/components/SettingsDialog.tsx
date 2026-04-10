import type { Settings } from "../lib/settings";

interface Props {
  settings: Settings;
  onChange: (settings: Settings) => void;
  onClose: () => void;
}

export function SettingsDialog({ settings, onChange, onClose }: Props) {
  const update = (patch: Partial<Settings>) => {
    onChange({ ...settings, ...patch });
  };

  return (
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div class="bg-bg-secondary border border-border rounded-xl w-full max-w-md p-6 shadow-xl">
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
          <h3 class="text-sm font-medium text-text mb-3">Theme</h3>
          <div class="flex gap-2">
            {(["auto", "dark", "light"] as const).map((t) => (
              <button
                key={t}
                onClick={() => update({ theme: t })}
                class={`px-4 py-2 text-sm rounded-lg border cursor-pointer transition-colors ${
                  settings.theme === t
                    ? "bg-accent text-white border-accent"
                    : "bg-bg border-border text-text-secondary hover:text-text"
                }`}
              >
                {t === "auto" ? "Auto" : t === "dark" ? "Dark" : "Light"}
              </button>
            ))}
          </div>
        </section>

        {/* Chat display */}
        <section>
          <h3 class="text-sm font-medium text-text mb-3">Chat Display</h3>
          <div class="space-y-3">
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
    <label class="flex items-center justify-between cursor-pointer">
      <div>
        <div class="text-sm text-text">{label}</div>
        <div class="text-xs text-text-muted">{description}</div>
      </div>
      <button
        onClick={() => onChange(!checked)}
        class={`relative w-10 h-6 rounded-full transition-colors cursor-pointer ${
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
