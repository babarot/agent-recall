import { useState, useEffect } from "preact/hooks";
import { SessionList } from "./components/SessionList";
import { ChatView } from "./components/ChatView";
import { StatsView } from "./components/StatsView";

type View = "list" | "chat" | "stats";

interface Session {
  sessionId: string;
  fullSessionId: string;
  project: string;
  branch: string;
  firstPrompt: string;
  messages: number;
  date: string;
}

export function App() {
  const [view, setView] = useState<View>("list");
  const [selectedSession, setSelectedSession] = useState<string | null>(null);
  const [stats, setStats] = useState<unknown>(null);

  useEffect(() => {
    fetch("/api/stats")
      .then((r) => r.json())
      .then(setStats);
  }, []);

  const handleSelectSession = (sessionId: string) => {
    setSelectedSession(sessionId);
    setView("chat");
  };

  const handleBack = () => {
    setSelectedSession(null);
    setView("list");
  };

  return (
    <>
      <header class="h-12 shrink-0 flex items-center justify-between px-5 border-b border-border bg-bg-secondary">
        <button
          onClick={() => { setView("list"); setSelectedSession(null); }}
          class="text-base font-semibold text-text hover:text-accent transition-colors cursor-pointer"
        >
          agent-recall
        </button>
        <nav class="flex gap-1">
          <NavButton active={view === "list" || view === "chat"} onClick={() => handleBack()}>
            Sessions
          </NavButton>
          <NavButton active={view === "stats"} onClick={() => setView("stats")}>
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
