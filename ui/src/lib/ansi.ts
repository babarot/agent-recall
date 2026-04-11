// ANSI SGR (Select Graphic Rendition) -> HTML converter.
// Only supports CSI `ESC [ ... m` sequences. Other escape sequences
// (cursor movement, OSC, etc.) are left untouched — they do not appear
// in the tool-result output we care about.

// VS Code integrated-terminal palette — reads well on both light/dark themes.
const FG_COLORS: Record<number, string> = {
  30: "#000000", 31: "#cd3131", 32: "#0dbc79", 33: "#e5e510",
  34: "#2472c8", 35: "#bc3fbc", 36: "#11a8cd", 37: "#e5e5e5",
  90: "#666666", 91: "#f14c4c", 92: "#23d18b", 93: "#f5f543",
  94: "#3b8eea", 95: "#d670d6", 96: "#29b8db", 97: "#ffffff",
};

const BG_COLORS: Record<number, string> = {
   40: "#000000",  41: "#cd3131",  42: "#0dbc79",  43: "#e5e510",
   44: "#2472c8",  45: "#bc3fbc",  46: "#11a8cd",  47: "#e5e5e5",
  100: "#666666", 101: "#f14c4c", 102: "#23d18b", 103: "#f5f543",
  104: "#3b8eea", 105: "#d670d6", 106: "#29b8db", 107: "#ffffff",
};

interface Style {
  fg?: string;
  bg?: string;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
}

function ansi256ToHex(n: number): string {
  if (n < 8) return FG_COLORS[30 + n] ?? "#ffffff";
  if (n < 16) return FG_COLORS[90 + (n - 8)] ?? "#ffffff";
  if (n < 232) {
    const i = n - 16;
    const r = Math.floor(i / 36);
    const g = Math.floor((i % 36) / 6);
    const b = i % 6;
    const to255 = (v: number) => (v === 0 ? 0 : 55 + v * 40);
    const hex = (v: number) => to255(v).toString(16).padStart(2, "0");
    return `#${hex(r)}${hex(g)}${hex(b)}`;
  }
  // 232-255 grayscale ramp
  const v = 8 + (n - 232) * 10;
  const h = v.toString(16).padStart(2, "0");
  return `#${h}${h}${h}`;
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => {
    switch (c) {
      case "&": return "&amp;";
      case "<": return "&lt;";
      case ">": return "&gt;";
      case '"': return "&quot;";
      case "'": return "&#39;";
      default: return c;
    }
  });
}

function styleIsEmpty(s: Style): boolean {
  return !s.fg && !s.bg && !s.bold && !s.dim && !s.italic && !s.underline;
}

function styleToCss(s: Style): string {
  const parts: string[] = [];
  if (s.fg) parts.push(`color:${s.fg}`);
  if (s.bg) parts.push(`background-color:${s.bg}`);
  if (s.bold) parts.push("font-weight:bold");
  if (s.dim) parts.push("opacity:0.7");
  if (s.italic) parts.push("font-style:italic");
  if (s.underline) parts.push("text-decoration:underline");
  return parts.join(";");
}

function applyCodes(state: Style, codes: number[]): Style {
  let s: Style = { ...state };
  let i = 0;
  while (i < codes.length) {
    const c = codes[i];
    if (c === 0) { s = {}; i++; continue; }
    if (c === 1) { s.bold = true; i++; continue; }
    if (c === 2) { s.dim = true; i++; continue; }
    if (c === 3) { s.italic = true; i++; continue; }
    if (c === 4) { s.underline = true; i++; continue; }
    if (c === 22) { s.bold = false; s.dim = false; i++; continue; }
    if (c === 23) { s.italic = false; i++; continue; }
    if (c === 24) { s.underline = false; i++; continue; }
    if (c === 39) { s.fg = undefined; i++; continue; }
    if (c === 49) { s.bg = undefined; i++; continue; }
    if ((c >= 30 && c <= 37) || (c >= 90 && c <= 97)) {
      s.fg = FG_COLORS[c];
      i++;
      continue;
    }
    if ((c >= 40 && c <= 47) || (c >= 100 && c <= 107)) {
      s.bg = BG_COLORS[c];
      i++;
      continue;
    }
    if (c === 38 && codes[i + 1] === 5 && codes[i + 2] !== undefined) {
      s.fg = ansi256ToHex(codes[i + 2]);
      i += 3;
      continue;
    }
    if (c === 48 && codes[i + 1] === 5 && codes[i + 2] !== undefined) {
      s.bg = ansi256ToHex(codes[i + 2]);
      i += 3;
      continue;
    }
    if (c === 38 && codes[i + 1] === 2 && codes.length >= i + 5) {
      const r = codes[i + 2], g = codes[i + 3], b = codes[i + 4];
      s.fg = `rgb(${r},${g},${b})`;
      i += 5;
      continue;
    }
    if (c === 48 && codes[i + 1] === 2 && codes.length >= i + 5) {
      const r = codes[i + 2], g = codes[i + 3], b = codes[i + 4];
      s.bg = `rgb(${r},${g},${b})`;
      i += 5;
      continue;
    }
    // Unknown — skip one code and keep going.
    i++;
  }
  return s;
}

const SGR_RE = /\x1b\[([\d;]*)m/g;

/** Returns true if the input contains at least one CSI escape sequence. */
export function hasAnsi(input: string): boolean {
  return /\x1b\[/.test(input);
}

/**
 * Convert a string containing ANSI SGR escape sequences into HTML with
 * inline-styled `<span>` tags. Text content is HTML-escaped. Style values
 * come from a fixed palette / integer parsing, so the output is safe to
 * inject via `dangerouslySetInnerHTML`.
 */
export function ansiToHtml(input: string): string {
  if (!input) return "";
  let out = "";
  let state: Style = {};
  let lastIndex = 0;

  const writeSegment = (text: string) => {
    if (!text) return;
    const esc = escapeHtml(text);
    if (styleIsEmpty(state)) {
      out += esc;
    } else {
      out += `<span style="${styleToCss(state)}">${esc}</span>`;
    }
  };

  SGR_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = SGR_RE.exec(input)) !== null) {
    writeSegment(input.slice(lastIndex, m.index));
    const codes = m[1] === ""
      ? [0]
      : m[1].split(";").map((n) => parseInt(n, 10) || 0);
    state = applyCodes(state, codes);
    lastIndex = SGR_RE.lastIndex;
  }
  writeSegment(input.slice(lastIndex));
  return out;
}
