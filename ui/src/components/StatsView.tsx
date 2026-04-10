interface StatsData {
  totalSessions: number;
  totalMessages: number;
  byProject: Array<{ project: string; sessions: number; messages: number }>;
  byMonth: Array<{ month: string; sessions: number; messages: number }>;
}

export function StatsView({ data }: { data: unknown }) {
  if (!data) {
    return <div class="flex items-center justify-center h-full text-text-secondary">Loading...</div>;
  }

  const stats = data as StatsData;

  return (
    <div class="h-full overflow-y-auto p-6">
      <div class="max-w-4xl mx-auto space-y-8">
        {/* Overview */}
        <div class="grid grid-cols-2 gap-4">
          <StatCard label="Total Sessions" value={stats.totalSessions} />
          <StatCard label="Total Messages" value={stats.totalMessages} />
        </div>

        {/* By Project */}
        <section>
          <h2 class="text-base font-semibold text-text mb-3">By Project</h2>
          <div class="space-y-1">
            {stats.byProject?.map((p) => (
              <div key={p.project} class="flex items-center justify-between py-2 px-3 rounded-md hover:bg-bg-secondary">
                <span class="text-sm text-text truncate mr-4">{p.project}</span>
                <div class="flex gap-6 text-xs text-text-muted shrink-0">
                  <span>{p.sessions} sessions</span>
                  <span>{p.messages} messages</span>
                </div>
              </div>
            ))}
          </div>
        </section>

        {/* By Month */}
        <section>
          <h2 class="text-base font-semibold text-text mb-3">By Month</h2>
          <div class="space-y-1">
            {stats.byMonth?.map((m) => (
              <div key={m.month} class="flex items-center justify-between py-2 px-3 rounded-md hover:bg-bg-secondary">
                <span class="text-sm text-text font-mono">{m.month}</span>
                <div class="flex gap-6 text-xs text-text-muted shrink-0">
                  <span>{m.sessions} sessions</span>
                  <span>{m.messages} messages</span>
                </div>
              </div>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: number }) {
  return (
    <div class="bg-bg-secondary border border-border rounded-lg p-5">
      <div class="text-2xl font-bold text-text">{value.toLocaleString()}</div>
      <div class="text-sm text-text-secondary mt-1">{label}</div>
    </div>
  );
}
