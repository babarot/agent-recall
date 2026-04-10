import { bold, dim } from "@std/fmt/colors";
import { VaultDB } from "./db.ts";
import { displayProject } from "./display.ts";

interface StatsOptions {
  dbPath: string;
  project?: string;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function runStats(options: StatsOptions): void {
  const db = new VaultDB(options.dbPath);
  const s = db.stats(options.project);
  db.close();

  console.log(bold("Archive Statistics"));
  console.log(`  Total sessions:  ${s.totalSessions}`);
  console.log(`  Total messages:  ${s.totalMessages}`);
  console.log(`  Database size:   ${formatBytes(s.dbSizeBytes)}`);

  if (s.byProject.length > 0) {
    console.log(`\n${bold("By Project:")}`);
    for (const p of s.byProject) {
      const projectFull = displayProject(p.projectPath, p.project);
      const projectShort = projectFull.length > 40
        ? "..." + projectFull.slice(-37)
        : projectFull;
      console.log(
        `  ${projectShort.padEnd(42)} ${String(p.sessions).padStart(4)} sessions  ${String(p.messages).padStart(6)} messages`
      );
    }
  }

  if (s.byMonth.length > 0) {
    console.log(`\n${bold("By Month:")}`);
    for (const m of s.byMonth) {
      console.log(
        `  ${dim(m.month)}  ${String(m.sessions).padStart(4)} sessions  ${String(m.messages).padStart(6)} messages`
      );
    }
  }
}
