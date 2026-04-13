import { describe, it, expect } from "vitest";
import { formatDate, formatDateTime } from "./format-date";

// ISO inputs include an explicit timezone offset so results don't depend on
// the machine running the tests. "2026-04-13T22:47:00+09:00" is unambiguous
// and rendered in local time — which is precisely what the UI wants.

describe("formatDate", () => {
  it("formats an ISO string with explicit offset as YYYY-MM-DD in local time", () => {
    // 22:47 JST on 2026-04-13 — local-time date always ≥ 2026-04-13 for any
    // TZ at or east of UTC-13, which covers every real-world zone.
    expect(formatDate("2026-04-13T22:47:00+09:00")).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });

  it("zero-pads single-digit months and days", () => {
    // Pick an hour well inside the day so TZ shifts don't kick it over
    // to an adjacent day.
    const out = formatDate("2026-01-05T12:00:00+00:00");
    // Regardless of TZ, month and day should both be 2 chars.
    const parts = out.split("-");
    expect(parts).toHaveLength(3);
    expect(parts[1]).toHaveLength(2);
    expect(parts[2]).toHaveLength(2);
  });

  it("returns empty string for undefined/null/empty", () => {
    expect(formatDate(undefined)).toBe("");
    expect(formatDate(null)).toBe("");
    expect(formatDate("")).toBe("");
  });

  it("returns empty string for invalid ISO input", () => {
    expect(formatDate("not-a-date")).toBe("");
  });
});

describe("formatDateTime", () => {
  it("formats as YYYY-MM-DD HH:MM in local time", () => {
    const out = formatDateTime("2026-04-13T22:47:00+09:00");
    // Shape check — exact digits depend on runner's TZ.
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
  });

  it("zero-pads single-digit hours and minutes", () => {
    const out = formatDateTime("2026-04-13T12:00:00+00:00");
    const timePart = out.split(" ")[1];
    expect(timePart).toMatch(/^\d{2}:\d{2}$/);
  });

  it("returns empty string for undefined/null/empty", () => {
    expect(formatDateTime(undefined)).toBe("");
    expect(formatDateTime(null)).toBe("");
    expect(formatDateTime("")).toBe("");
  });

  it("returns empty string for invalid ISO input", () => {
    expect(formatDateTime("not-a-date")).toBe("");
  });
});
