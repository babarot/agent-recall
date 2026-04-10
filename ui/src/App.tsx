import { useState, useEffect, useCallback } from "preact/hooks";
import { SessionList } from "./components/SessionList";
import { ChatView } from "./components/ChatView";
import { StatsView } from "./components/StatsView";

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

  useEffect(() => {
    fetch("/api/stats")
      .then((r) => r.json())
      .then(setStats);
  }, []);

  // Listen for browser back/forward
  useEffect(() => {
    const onPopState = () => {
      const route = parseRoute();
      setView(route.view);
      setSelectedSession(route.sessionId);
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

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

  return (
    <>
      <header class="h-12 shrink-0 flex items-center justify-between px-5 border-b border-border bg-bg-secondary">
        <button
          onClick={handleBack}
          class="text-base font-semibold text-text hover:text-accent transition-colors cursor-pointer"
        >
          agent-recall
        </button>
        <nav class="flex gap-1">
          <NavButton active={view === "list" || view === "chat"} onClick={handleBack}>
            Sessions
          </NavButton>
          <NavButton active={view === "stats"} onClick={handleStats}>
            Stats
          </NavButton>
        </nav>
      </header>

      <main class="flex-1 overflow-hidden">
        {view === "list" && (
          <SessionList onSelect={handleSelectSession} />
        )}
        {view === "chat" && selectedSession && (
          <ChatView sessionId={selectedSession} onBack={handleBack} />
        )}
        {view === "stats" && (
          <StatsView data={stats} />
        )}
      </main>
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
      class={`px-3 py-1.5 text-sm rounded-md transition-colors cursor-pointer ${
        active
          ? "bg-bg-tertiary text-text"
          : "text-text-secondary hover:text-text hover:bg-bg-tertiary"
      }`}
    >
      {children}
    </button>
  );
}
