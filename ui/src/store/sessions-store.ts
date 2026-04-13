/**
 * Sessions list data layer.
 *
 * Owns everything that used to live as component state in SessionList:
 * the list itself, the search query, the project filter, the committed
 * search mode, and the backing stats/projects data. Components read via
 * signals and call action functions to mutate — the store persists
 * across view navigation, so returning to the list no longer triggers
 * a refetch or loses scroll state.
 *
 * The store also owns its SSE subscription so live updates keep flowing
 * even while the user is in the chat or stats view.
 */

import { signal, computed } from "@preact/signals";
import { subscribeSSE } from "../lib/sse-bus";
import type { SSEEvent } from "../lib/sse-bus";

export interface Session {
  sessionId: string;
  fullSessionId: string;
  project: string;
  branch: string;
  firstPrompt: string;
  lastPrompt: string;
  messages: number;
  date: string;
  createdAt?: string;
  updatedAt?: string;
  activity: number[];
}

export interface Project {
  display: string;
  value: string;
}

interface ProjectStat {
  project: string;
  projectPath: string;
  sessions: number;
  messages: number;
}

export interface StatsData {
  totalSessions: number;
  totalMessages: number;
  byProject?: ProjectStat[];
  byMonth?: Array<{ month: string; sessions: number; messages: number }>;
}

const PAGE_SIZE = 50;

export const sessions = signal<Session[]>([]);
export const stats = signal<StatsData | null>(null);
export const query = signal("");
export const committedQuery = signal("");
export const project = signal("");
export const loading = signal(false);
export const hasMore = signal(true);

export const projects = computed<Project[]>(() =>
  stats.value?.byProject?.map((p) => ({
    display: p.project,
    value: p.projectPath,
  })) ?? []
);

// Side-effect guards. These track in-flight fetches so concurrent callers
// de-dup, but do NOT gate retries on the "has been attempted once" bit —
// a failed fetch leaves flags clean so the next init() retries naturally.
let sseSubscribed = false;
const loadingRef = { current: false };
const loadingStatsRef = { current: false };

/**
 * Idempotent startup. Called from SessionList and StatsView on mount.
 * Subscribes to SSE once per tab, and fires the initial fetches when
 * the store is empty. Safe to call repeatedly — concurrent/duplicate
 * calls are de-duped, and transient errors are recoverable by simply
 * calling init() again (e.g. when the user navigates back to the list).
 */
export function init(): void {
  if (!sseSubscribed) {
    sseSubscribed = true;
    subscribeSSE(handleSSE);
  }
  if (sessions.value.length === 0 && !loadingRef.current) {
    void fetchSessions(true);
  }
  if (stats.value === null && !loadingStatsRef.current) {
    void fetchStats();
  }
}

async function fetchStats(): Promise<void> {
  if (loadingStatsRef.current) return;
  loadingStatsRef.current = true;
  try {
    const res = await fetch("/api/stats");
    if (!res.ok) throw new Error(`fetch failed: ${res.status}`);
    stats.value = await res.json();
  } catch {
    // Transient; next init() retries while stats.value stays null.
  } finally {
    loadingStatsRef.current = false;
  }
}

export async function fetchSessions(reset: boolean): Promise<void> {
  if (loadingRef.current) return;
  loadingRef.current = true;
  loading.value = true;
  try {
    const offset = reset ? 0 : sessions.value.length;
    const params = new URLSearchParams();
    if (project.value) params.set("project", project.value);
    params.set("limit", String(PAGE_SIZE));
    params.set("offset", String(offset));
    const res = await fetch(`/api/sessions?${params}`);
    if (!res.ok) throw new Error(`fetch failed: ${res.status}`);
    const data: Session[] = await res.json();
    sessions.value = reset ? data : [...sessions.value, ...data];
    hasMore.value = data.length >= PAGE_SIZE;
  } catch {
    // Transient. Leave the existing list intact so the user still sees
    // whatever was there; the next interaction (nav, SSE, manual reload)
    // retries.
    hasMore.value = false;
  } finally {
    loading.value = false;
    loadingRef.current = false;
  }
}

export async function runSearch(q: string): Promise<void> {
  if (!q.trim()) {
    // Empty query behaves like clearing the search: drop back to the live list.
    committedQuery.value = "";
    await fetchSessions(true);
    return;
  }
  loading.value = true;
  committedQuery.value = q;
  try {
    const params = new URLSearchParams({ q, limit: "100" });
    if (project.value) params.set("project", project.value);
    const res = await fetch(`/api/search?${params}`);
    if (!res.ok) throw new Error(`search failed: ${res.status}`);
    const results = (await res.json()) as Array<{
      sessionId: string;
      project: string;
      branch: string;
      content?: string;
      date: string;
    }>;
    const map = new Map<string, Session>();
    for (const r of results) {
      if (!map.has(r.sessionId)) {
        map.set(r.sessionId, {
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
    sessions.value = Array.from(map.values());
    hasMore.value = false;
  } catch {
    // Keep previous sessions; user can retry.
  } finally {
    loading.value = false;
  }
}

export function setProject(p: string): void {
  project.value = p;
  void fetchSessions(true);
}

/**
 * Called on every keystroke in the search box. Clearing the input
 * immediately drops the frozen search-result view and returns to the
 * live list — typing without committing (no Enter) does NOT freeze
 * live updates.
 */
export function setQuery(q: string): void {
  query.value = q;
  if (q === "" && committedQuery.value !== "") {
    committedQuery.value = "";
    void fetchSessions(true);
  }
}

function handleSSE(event: SSEEvent): void {
  // While a committed search is active, freeze the result set so SSE
  // updates don't reshuffle the search view under the user.
  if (committedQuery.value !== "") return;
  if (event.type !== "session_updated") return;
  const sessionId = event.sessionId as string | undefined;
  if (!sessionId) return;

  // Re-fetch the full row from the top of the list so firstPrompt /
  // lastPrompt / messages stay current, then splice it to the top.
  const params = new URLSearchParams();
  if (project.value) params.set("project", project.value);
  params.set("limit", "1");
  params.set("offset", "0");
  fetch(`/api/sessions?${params}`)
    .then((r) => r.json())
    .then((data: Session[]) => {
      const fresh = data[0];
      if (!fresh || fresh.fullSessionId !== sessionId) return;
      const without = sessions.value.filter(
        (s) => s.fullSessionId !== sessionId,
      );
      sessions.value = [fresh, ...without];
    })
    .catch(() => {
      // Transient; next event will retry.
    });
}

/** Test-only: reset every signal and side-effect flag. */
export function __resetStoreForTests(): void {
  sessions.value = [];
  stats.value = null;
  query.value = "";
  committedQuery.value = "";
  project.value = "";
  loading.value = false;
  hasMore.value = true;
  sseSubscribed = false;
  loadingRef.current = false;
  loadingStatsRef.current = false;
}
