import { useState, useEffect, useCallback } from "preact/hooks";
import { Settings as SettingsIcon, Brain } from "lucide-preact";
import { SessionList } from "./components/SessionList";
import { ChatView } from "./components/ChatView";
import { StatsView } from "./components/StatsView";
import { SettingsDialog } from "./components/SettingsDialog";
import { loadSettings, saveSettings, applyAllSettings } from "./lib/settings";
import type { Settings } from "./lib/settings";
import { useKeyboardShortcut } from "./hooks/use-keyboard-shortcut";

type View = "list" | "chat" | "stats";

function parseRoute(): { view: View; sessionId: string | null } {
  const path = window.location.pathname;
  if (path === "/stats") return { view: "stats", sessionId: null };
  const match = path.match(/^\/session\/(.+)$/);
  if (match) return { view: "chat", sessionId: match[1] };
  return { view: "list", sessionId: null };
}

export function App() {
  const [view, setView] = useState<View>(() => parseRoute().view);
  const [selectedSession, setSelectedSession] = useState<string | null>(() => parseRoute().sessionId);
  const [stats, setStats] = useState<unknown>(null);
  const [settings, setSettings] = useState<Settings>(loadSettings);
  const [showSettings, setShowSettings] = useState(false);

  useEffect(() => {
    applyAllSettings(settings);
  }, [settings.theme, settings.colorScheme]);

  useEffect(() => {
    fetch("/api/stats")
      .then((r) => r.json())
      .then(setStats);
  }, []);

  useEffect(() => {
    const onPopState = () => {
      const route = parseRoute();
      setView(route.view);
      setSelectedSession(route.sessionId);
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useKeyboardShortcut("Escape", () => setShowSettings(false));

  const navigate = useCallback((path: string, view: View, sessionId: string | null) => {
    window.history.pushState(null, "", path);
    setView(view);
    setSelectedSession(sessionId);
  }, []);

  const handleSelectSession = (sessionId: string) => {
    navigate(`/session/${sessionId}`, "chat", sessionId);
  };

  const handleBack = () => {
    navigate("/", "list", null);
  };

  const handleStats = () => {
    navigate("/stats", "stats", null);
  };

  const handleSettingsChange = (newSettings: Settings) => {
    setSettings(newSettings);
    saveSettings(newSettings);
    applyAllSettings(newSettings);
  };

  return (
    <>
      <header class="h-14 shrink-0 flex items-center justify-between px-6 border-b border-border/60 bg-bg-secondary/80 backdrop-blur-sm">
        <button
          onClick={handleBack}
          class="flex items-center gap-2 group cursor-pointer"
        >
          <div class="w-7 h-7 rounded-lg bg-accent/15 flex items-center justify-center group-hover:bg-accent/25 transition-colors">
            <Brain size={14} class="text-accent" />
          </div>
          <span class="text-sm font-semibold text-text tracking-tight group-hover:text-accent transition-colors">
            Agent Recall
          </span>
        </button>
        <nav class="flex gap-1 items-center">
          <NavButton active={view === "list" || view === "chat"} onClick={handleBack}>
            Sessions
          </NavButton>
          <NavButton active={view === "stats"} onClick={handleStats}>
            Stats
          </NavButton>
          <div class="w-px h-5 bg-border/50 mx-1.5" />
          <button
            onClick={() => setShowSettings(true)}
            class="p-2 text-text-muted hover:text-text hover:bg-bg-tertiary rounded-lg transition-colors cursor-pointer"
            title="Settings"
          >
            <SettingsIcon size={15} />
          </button>
        </nav>
      </header>

      <main class="flex-1 overflow-hidden">
        {view === "list" && (
          <SessionList onSelect={handleSelectSession} settings={settings} />
        )}
        {view === "chat" && selectedSession && (
          <ChatView sessionId={selectedSession} onBack={handleBack} settings={settings} />
        )}
        {view === "stats" && (
          <StatsView data={stats} />
        )}
      </main>

      {showSettings && (
        <SettingsDialog
          settings={settings}
          onChange={handleSettingsChange}
          onClose={() => setShowSettings(false)}
        />
      )}
    </>
  );
}

function NavButton({ active, onClick, children }: {
  active: boolean;
  onClick: () => void;
  children: preact.ComponentChildren;
}) {
  return (
    <button
      onClick={onClick}
      class={`px-3 py-1.5 text-sm font-medium rounded-lg transition-all cursor-pointer ${
        active
          ? "bg-accent/10 text-accent"
          : "text-text-muted hover:text-text hover:bg-bg-tertiary"
      }`}
    >
      {children}
    </button>
  );
}
