/**
 * Date / datetime formatting for the session list UI.
 *
 * Inputs are ISO-8601 strings (the shape DB/API emits). Output is in the
 * viewer's local timezone — sessions in the list feel "today/yesterday"
 * based on the user's wall clock, not UTC.
 */

/** Format ISO string → "YYYY-MM-DD" in local time. Empty/invalid → "". */
export function formatDate(iso: string | undefined | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

/** Format ISO string → "YYYY-MM-DD HH:MM" in local time. Empty/invalid → "". */
export function formatDateTime(iso: string | undefined | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const date = formatDate(iso);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${date} ${hh}:${mm}`;
}
