import { join } from "@std/path";

function homeDir(): string {
  return Deno.env.get("HOME") ?? "/tmp";
}

export const CLAUDE_DIR = join(homeDir(), ".claude");
export const PROJECTS_DIR = join(CLAUDE_DIR, "projects");
export const DEFAULT_DB_PATH = join(CLAUDE_DIR, "vault.db");
