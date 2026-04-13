import { useEffect, useRef, useCallback } from "preact/hooks";
import { Search } from "lucide-preact";
import { AreaChart, Area, YAxis, ResponsiveContainer } from "recharts";
import type { Settings } from "../lib/settings";
import { formatDate, formatDateTime } from "../lib/format-date";
import {
  sessions as sessionsSignal,
  query as querySignal,
  project as projectSignal,
  projects as projectsComputed,
  loading as loadingSignal,
  hasMore as hasMoreSignal,
  init,
  fetchSessions,
  runSearch,
  setProject,
  setQuery,
} from "../store/sessions-store";

export function SessionList({ onSelect, settings }: { onSelect: (id: string) => void; settings: Settings }) {
  const searchRef = useRef<HTMLInputElement>(null);
  const loadingRef = useRef(false);

  // Reactive reads from the store. Components just render what the store
  // holds; the store persists across navigation so returning here no
  // longer triggers a refetch or resets scroll/filter state.
  const sessions = sessionsSignal.value;
  const query = querySignal.value;
  const project = projectSignal.value;
  const projects = projectsComputed.value;
  const loading = loadingSignal.value;
  const hasMore = hasMoreSignal.value;

  // Kick the store on every mount. init() is idempotent and retries any
  // transient fetch failure from a previous mount — see sessions-store.ts.
  useEffect(() => {
    init();
  }, []);

  // Keep loadingRef synced for the IntersectionObserver closure below.
  loadingRef.current = loading;

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

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter") void runSearch(query);
  };

  // Infinite scroll sentinel
  const sentinelRef = useCallback(
    (node: HTMLDivElement | null) => {
      if (!node) return;
      const observer = new IntersectionObserver(
        (entries) => {
          if (entries[0].isIntersecting && hasMore && !loadingRef.current) {
            void fetchSessions(false);
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
              onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
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
                  <span class="text-xs text-text-muted shrink-0 ml-3">{s.messages} msgs</span>
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
                  <span>{formatDate(s.createdAt) || s.date}</span>
                  {formatDateTime(s.updatedAt) && (
                    <>
                      <span class="text-text-muted/50">→</span>
                      <span>{formatDateTime(s.updatedAt)}</span>
                    </>
                  )}
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
