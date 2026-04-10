import { useState, useEffect, useRef } from "preact/hooks";

interface Session {
  sessionId: string;
  fullSessionId: string;
  project: string;
  branch: string;
  firstPrompt: string;
  messages: number;
  date: string;
}

export function SessionList({ onSelect }: { onSelect: (id: string) => void }) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [query, setQuery] = useState("");
  const [project, setProject] = useState("");
  const [projects, setProjects] = useState<Array<{ display: string; value: string }>>([]);
  const [loading, setLoading] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    fetchSessions();
    // Get project list from stats
    fetch("/api/stats")
      .then((r) => r.json())
      .then((data) => {
        setProjects(data.byProject?.map((p: { project: string; projectPath: string }) => ({
          display: p.project,
          value: p.projectPath,
        })) ?? []);
      });
  }, []);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "/" && document.activeElement !== searchRef.current) {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const fetchSessions = async () => {
    setLoading(true);
    const params = new URLSearchParams();
    if (project) params.set("project", project);
    params.set("limit", "100");
    const res = await fetch(`/api/sessions?${params}`);
    setSessions(await res.json());
    setLoading(false);
  };

  const handleSearch = async () => {
    if (!query.trim()) {
      fetchSessions();
      return;
    }
    setLoading(true);
    const params = new URLSearchParams({ q: query, limit: "50" });
    if (project) params.set("project", project);
    const res = await fetch(`/api/search?${params}`);
    const results = await res.json();
    // Group search results by session
    const sessionMap = new Map<string, Session>();
    for (const r of results) {
      if (!sessionMap.has(r.sessionId)) {
        sessionMap.set(r.sessionId, {
          sessionId: r.sessionId,
          fullSessionId: r.sessionId,
          project: r.project,
          branch: r.branch,
          firstPrompt: r.content?.slice(0, 200) ?? "",
          messages: 0,
          date: r.date,
        });
      }
    }
    setSessions(Array.from(sessionMap.values()));
    setLoading(false);
  };

  useEffect(() => {
    fetchSessions();
  }, [project]);

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter") handleSearch();
  };

  const copySessionId = (e: Event, fullId: string) => {
    e.stopPropagation();
    navigator.clipboard.writeText(fullId);
  };

  return (
    <div class="h-full flex flex-col">
      {/* Search bar */}
      <div class="p-4 border-b border-border bg-bg-secondary">
        <div class="flex gap-3 max-w-4xl mx-auto">
          <div class="flex-1 relative">
            <input
              ref={searchRef}
              type="text"
              value={query}
              onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
              onKeyDown={handleKeyDown}
              placeholder='Search sessions... (press "/" to focus)'
              class="w-full px-4 py-2 bg-bg border border-border rounded-lg text-text placeholder-text-muted focus:outline-none focus:border-accent text-sm"
            />
          </div>
          <select
            value={project}
            onChange={(e) => setProject((e.target as HTMLSelectElement).value)}
            class="px-3 py-2 bg-bg border border-border rounded-lg text-text text-sm focus:outline-none focus:border-accent"
          >
            <option value="">All projects</option>
            {projects.map((p) => (
              <option key={p.value} value={p.value}>{p.display}</option>
            ))}
          </select>
          <button
            onClick={handleSearch}
            class="px-4 py-2 bg-accent text-white rounded-lg text-sm font-medium hover:bg-accent-hover transition-colors cursor-pointer"
          >
            Search
          </button>
        </div>
      </div>

      {/* Session list */}
      <div class="flex-1 overflow-y-auto p-4">
        <div class="max-w-4xl mx-auto space-y-2">
          {loading && (
            <div class="text-center py-8 text-text-secondary">Loading...</div>
          )}
          {!loading && sessions.length === 0 && (
            <div class="text-center py-8 text-text-secondary">No sessions found.</div>
          )}
          {sessions.map((s) => (
            <div
              key={s.sessionId}
              onClick={() => onSelect(s.fullSessionId || s.sessionId)}
              class="p-4 bg-bg-secondary border border-border rounded-lg cursor-pointer hover:border-accent/50 transition-colors"
            >
              <div class="flex items-center justify-between mb-2">
                <div class="flex items-center gap-3">
                  <span class="text-sm font-mono text-accent">{s.sessionId.slice(0, 8)}</span>
                  <button
                    onClick={(e) => copySessionId(e, s.fullSessionId || s.sessionId)}
                    class="text-xs text-text-muted hover:text-text transition-colors cursor-pointer"
                    title="Copy session ID"
                  >
                    copy
                  </button>
                  <span class="text-sm text-text-secondary">{s.project}</span>
                </div>
                <div class="flex items-center gap-3 text-xs text-text-muted">
                  {s.branch && <span class="px-2 py-0.5 bg-bg-tertiary rounded">{s.branch}</span>}
                  <span>{s.messages} msgs</span>
                  <span>{s.date}</span>
                </div>
              </div>
              {s.firstPrompt && (
                <p class="text-sm text-text-secondary truncate">
                  {s.firstPrompt.slice(0, 150)}
                </p>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
