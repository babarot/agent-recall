import { useState, useEffect, useRef, useCallback } from "preact/hooks";
import { Search } from "lucide-preact";
import { AreaChart, Area, YAxis, ResponsiveContainer } from "recharts";
import { useSSE } from "../hooks/use-sse";
import type { Settings } from "../lib/settings";

interface Session {
  sessionId: string;
  fullSessionId: string;
  project: string;
  branch: string;
  firstPrompt: string;
  lastPrompt: string;
  messages: number;
  date: string;
  activity: number[];
}

const PAGE_SIZE = 50;

export function SessionList({ onSelect, settings }: { onSelect: (id: string) => void; settings: Settings }) {
  const [sessions, setSessions] = useState<Session[]>([]);
  // `query` is the raw text in the search box (updates on every keystroke).
  // `committedQuery` is the value that was last actually submitted via the
  // Search button / Enter key — this is what gates live updates. Separating
  // them ensures that *typing* into the search box does not freeze live
  // updates; only *committing* a non-empty search does.
  const [query, setQuery] = useState("");
  const [committedQuery, setCommittedQuery] = useState("");
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
      // Submitting an empty search behaves like clearing: drop back to the
      // normal live list and un-freeze SSE updates.
      setCommittedQuery("");
      fetchSessions(true);
      return;
    }
    setLoading(true);
    setCommittedQuery(query);
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
          lastPrompt: "",
          messages: 0,
          date: r.date,
          activity: [],
        });
      }
    }
    setSessions(Array.from(sessionMap.values()));
    setHasMore(false);
    setLoading(false);
  };

  // Called on every keystroke in the search input. If the user clears the
  // box completely, immediately drop the frozen search result and re-fetch
  // the live list so they don't stare at stale rows waiting for Enter.
  const handleQueryInput = (val: string) => {
    setQuery(val);
    if (val === "" && committedQuery !== "") {
      setCommittedQuery("");
      fetchSessions(true);
    }
  };

  useEffect(() => {
    fetchSessions(true);
  }, [project]);

  // Real-time updates via SSE.
  // - While a search has been committed (Enter / Search button), ignore
  //   events so we don't disrupt the frozen result set. Typing without
  //   committing still allows live updates — otherwise the list would
  //   mysteriously freeze the moment the user touches the search box.
  // - For both known and new sessions we re-fetch the single session from
  //   the API so that firstPrompt, lastPrompt, and message count are all
  //   up to date. The fresh row is placed at the top of the list.
  useSSE((event) => {
    if (committedQuery !== "") return;
    if (event.type !== "session_updated") return;

    const sessionId = event.sessionId as string | undefined;
    if (!sessionId) return;

    (async () => {
      try {
        const params = new URLSearchParams();
        if (project) params.set("project", project);
        params.set("limit", "1");
        params.set("offset", "0");
        const res = await fetch(`/api/sessions?${params}`);
        const data: Session[] = await res.json();
        const fresh = data[0];
        if (!fresh || fresh.fullSessionId !== sessionId) return;
        setSessions((prev) => {
          const without = prev.filter((s) => s.fullSessionId !== sessionId);
          return [fresh, ...without];
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
      {/* Search & filter — visually continues the header */}
      <div class="px-4 py-2.5 bg-bg-secondary/40">
        <div class="flex gap-2 max-w-4xl mx-auto items-center">
          <div class="flex-1 relative">
            <Search size={14} class="absolute left-3 top-1/2 -translate-y-1/2 text-text-muted pointer-events-none" />
            <input
              ref={searchRef}
              type="text"
              value={query}
              onInput={(e) => handleQueryInput((e.target as HTMLInputElement).value)}
              onKeyDown={handleKeyDown}
              placeholder="Search sessions..."
              class="w-full pl-9 pr-8 py-1.5 bg-bg-tertiary/50 border border-transparent rounded-md text-text placeholder-text-muted focus:outline-none focus:border-accent/40 focus:bg-bg text-sm transition-all"
            />
            <kbd class="absolute right-2.5 top-1/2 -translate-y-1/2 text-[10px] text-text-muted/60 font-mono pointer-events-none">/</kbd>
          </div>
          <select
            value={project}
            onChange={(e) => setProject((e.target as HTMLSelectElement).value)}
            class="px-2.5 py-1.5 bg-bg-tertiary/50 border border-transparent rounded-md text-text-secondary text-sm focus:outline-none focus:border-accent/40 transition-colors cursor-pointer"
          >
            <option value="">All projects</option>
            {projects.map((p) => (
              <option key={p.value} value={p.value}>{p.display}</option>
            ))}
          </select>
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
          {(() => {
            const allValues = sessions.flatMap((s) => s.activity).sort((a, b) => a - b);
            // Use P95 as the Y-axis ceiling so outlier sessions don't flatten
            // everything else. Values above P95 simply clip at the card top.
            const p95 = allValues.length > 0
              ? allValues[Math.floor(allValues.length * 0.95)]
              : 1;
            const globalMax = Math.max(1, p95);
            return sessions.map((s) => (
            <div
              key={s.fullSessionId || s.sessionId}
              onClick={() => onSelect(s.fullSessionId || s.sessionId)}
              class="relative p-4 bg-bg-secondary border border-border rounded-lg cursor-pointer hover:border-accent/50 transition-colors overflow-hidden"
            >
              {settings.showSparkline && s.activity.length > 0 && (
                <Sparkline data={s.activity} globalMax={globalMax} />
              )}
              <div class="relative">
                <div class="flex items-center justify-between mb-1.5">
                  <div class="flex items-center gap-2 min-w-0">
                    <span class="text-sm font-medium text-text truncate">{s.project}</span>
                    {s.branch && <span class="text-xs px-1.5 py-0.5 bg-bg-tertiary rounded text-text-muted shrink-0">{s.branch}</span>}
                  </div>
                  <span class="text-xs text-text-muted shrink-0 ml-3">{s.date}</span>
                </div>
                <p class="text-sm truncate mb-2">
                  {(() => {
                    const prompt = settings.startAtBottom ? (s.lastPrompt || s.firstPrompt) : s.firstPrompt;
                    return prompt
                      ? <span class="text-text-secondary">{prompt}</span>
                      : <span class="text-text-muted italic">Started with slash command</span>;
                  })()}
                </p>
                <div class="flex items-center gap-2 text-xs text-text-muted">
                  <span class="font-mono">{s.sessionId.slice(0, 8)}</span>
                  <span>{s.messages} msgs</span>
                </div>
              </div>
            </div>
          ));
          })()}
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

/** Recharts sparkline rendered as a card background. */
function Sparkline({ data, globalMax }: { data: number[]; globalMax: number }) {
  if (data.length < 2 || globalMax === 0) return null;

  const chartData = data.map((v) => ({ v }));

  return (
    <div class="absolute inset-0 pointer-events-none">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={chartData} margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="spark-fill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--color-accent)" stopOpacity={0.08} />
              <stop offset="100%" stopColor="var(--color-accent)" stopOpacity={0.01} />
            </linearGradient>
          </defs>
          <YAxis domain={[0, globalMax]} hide />
          <Area
            type="monotone"
            dataKey="v"
            stroke="var(--color-accent)"
            strokeWidth={1.5}
            strokeOpacity={0.15}
            fill="url(#spark-fill)"
            isAnimationActive={false}
            dot={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
