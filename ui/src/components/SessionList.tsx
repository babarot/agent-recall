import { useState, useEffect, useRef, useCallback } from "preact/hooks";
import { useSSE } from "../hooks/use-sse";

interface Session {
  sessionId: string;
  fullSessionId: string;
  project: string;
  branch: string;
  firstPrompt: string;
  messages: number;
  date: string;
}

const PAGE_SIZE = 50;

export function SessionList({ onSelect }: { onSelect: (id: string) => void }) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [query, setQuery] = useState("");
  const [project, setProject] = useState("");
  const [projects, setProjects] = useState<Array<{ display: string; value: string }>>([]);
  const [loading, setLoading] = useState(false);
  const [hasMore, setHasMore] = useState(true);
  const searchRef = useRef<HTMLInputElement>(null);
  const loadingRef = useRef(false);

  useEffect(() => {
    fetchSessions(true);
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

  const fetchSessions = async (reset: boolean) => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    setLoading(true);

    const offset = reset ? 0 : sessions.length;
    const params = new URLSearchParams();
    if (project) params.set("project", project);
    params.set("limit", String(PAGE_SIZE));
    params.set("offset", String(offset));

    const res = await fetch(`/api/sessions?${params}`);
    const data: Session[] = await res.json();

    if (reset) {
      setSessions(data);
    } else {
      setSessions((prev) => [...prev, ...data]);
    }
    setHasMore(data.length >= PAGE_SIZE);
    setLoading(false);
    loadingRef.current = false;
  };

  const handleSearch = async () => {
    if (!query.trim()) {
      fetchSessions(true);
      return;
    }
    setLoading(true);
    const params = new URLSearchParams({ q: query, limit: "100" });
    if (project) params.set("project", project);
    const res = await fetch(`/api/search?${params}`);
    const results = await res.json();
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
    setHasMore(false);
    setLoading(false);
  };

  useEffect(() => {
    fetchSessions(true);
  }, [project]);

  // Real-time updates via SSE.
  // - While searching, ignore events so we don't disrupt the result set.
  // - For a session already on screen: update its message count and bubble
  //   it to the top (sessions are sorted by started_at desc anyway).
  // - For a brand-new session: refetch the top of the list and prepend it
  //   if the server now lists it first. We can't build a complete Session
  //   row from the SSE payload alone (no firstPrompt / date / branch).
  useSSE((event) => {
    if (query !== "") return;
    if (event.type !== "session_updated") return;

    const sessionId = event.sessionId as string | undefined;
    if (!sessionId) return;
    const totalMessages = event.totalMessages as number | undefined;

    let wasKnown = false;
    setSessions((prev) => {
      const idx = prev.findIndex((s) => s.fullSessionId === sessionId);
      if (idx === -1) return prev;
      wasKnown = true;
      const existing = prev[idx];
      const updated = {
        ...existing,
        messages: totalMessages ?? existing.messages,
      };
      return [updated, ...prev.slice(0, idx), ...prev.slice(idx + 1)];
    });

    if (wasKnown) return;

    // Unknown session → fetch the head of the list. If the new session is
    // actually the most recent, it will be position 0 of that response.
    (async () => {
      try {
        const params = new URLSearchParams();
        if (project) params.set("project", project);
        params.set("limit", "1");
        params.set("offset", "0");
        const res = await fetch(`/api/sessions?${params}`);
        const data: Session[] = await res.json();
        const first = data[0];
        if (!first || first.fullSessionId !== sessionId) return;
        setSessions((prev) => {
          if (prev.some((s) => s.fullSessionId === sessionId)) return prev;
          return [first, ...prev];
        });
      } catch {
        // Swallow — the next manual refresh will pick it up.
      }
    })();
  });

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter") handleSearch();
  };

  // Infinite scroll sentinel
  const sentinelRef = useCallback(
    (node: HTMLDivElement | null) => {
      if (!node) return;
      const observer = new IntersectionObserver(
        (entries) => {
          if (entries[0].isIntersecting && hasMore && !loadingRef.current) {
            fetchSessions(false);
          }
        },
        { rootMargin: "200px" }
      );
      observer.observe(node);
      return () => observer.disconnect();
    },
    [hasMore, sessions.length, project]
  );

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
          {loading && sessions.length === 0 && (
            <div class="text-center py-8 text-text-secondary">Loading...</div>
          )}
          {!loading && sessions.length === 0 && (
            <div class="text-center py-8 text-text-secondary">No sessions found.</div>
          )}
          {sessions.map((s) => (
            <div
              key={s.fullSessionId || s.sessionId}
              onClick={() => onSelect(s.fullSessionId || s.sessionId)}
              class="p-4 bg-bg-secondary border border-border rounded-lg cursor-pointer hover:border-accent/50 transition-colors"
            >
              <div class="flex items-center justify-between mb-1.5">
                <div class="flex items-center gap-2 min-w-0">
                  <span class="text-sm font-medium text-text truncate">{s.project}</span>
                  {s.branch && <span class="text-xs px-1.5 py-0.5 bg-bg-tertiary rounded text-text-muted shrink-0">{s.branch}</span>}
                </div>
                <span class="text-xs text-text-muted shrink-0 ml-3">{s.date}</span>
              </div>
              <p class="text-sm truncate mb-2">
                {s.firstPrompt
                  ? <span class="text-text-secondary">{s.firstPrompt}</span>
                  : <span class="text-text-muted italic">Started with slash command</span>
                }
              </p>
              <div class="flex items-center gap-2 text-xs text-text-muted">
                <span class="font-mono">{s.sessionId.slice(0, 8)}</span>
                <span>{s.messages} msgs</span>
              </div>
            </div>
          ))}
          {/* Sentinel for infinite scroll */}
          {hasMore && <div ref={sentinelRef} class="h-4" />}
          {loading && sessions.length > 0 && (
            <div class="text-center py-4 text-text-muted text-sm">Loading more...</div>
          )}
        </div>
      </div>
    </div>
  );
}
