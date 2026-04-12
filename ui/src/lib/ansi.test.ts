import { describe, it, expect } from "vitest";
import { ansiToHtml, hasAnsi } from "./ansi";

describe("hasAnsi", () => {
  it("returns true when input contains an ESC CSI sequence", () => {
    expect(hasAnsi("\x1b[32mhello\x1b[0m")).toBe(true);
  });

  it("returns false for plain text", () => {
    expect(hasAnsi("hello world")).toBe(false);
  });

  it("returns false for stripped bracket-only fragments", () => {
    // The bug report case: ESC was eaten by a prior consumer, leaving only `[0m`.
    expect(hasAnsi("[0m [38;5;245mhello")).toBe(false);
  });

  it("returns false for empty input", () => {
    expect(hasAnsi("")).toBe(false);
  });
});

describe("ansiToHtml", () => {
  it("returns empty string for empty input", () => {
    expect(ansiToHtml("")).toBe("");
  });

  it("passes plain text through, escaping HTML", () => {
    expect(ansiToHtml("hello <div>")).toBe("hello &lt;div&gt;");
  });

  it("wraps basic 16-color fg in a styled span", () => {
    expect(ansiToHtml("\x1b[32mok\x1b[0m")).toBe(
      '<span style="color:#0dbc79">ok</span>',
    );
  });

  it("wraps bright fg", () => {
    expect(ansiToHtml("\x1b[91mred\x1b[0m")).toBe(
      '<span style="color:#f14c4c">red</span>',
    );
  });

  it("handles background color", () => {
    expect(ansiToHtml("\x1b[41mBG\x1b[0m")).toBe(
      '<span style="background-color:#cd3131">BG</span>',
    );
  });

  it("handles 256-color grayscale (\\x1b[38;5;245m from the bug report)", () => {
    // n=245 -> v=8+(245-232)*10=138 -> #8a8a8a
    expect(ansiToHtml("\x1b[38;5;245m(1ms)\x1b[0m")).toBe(
      '<span style="color:#8a8a8a">(1ms)</span>',
    );
  });

  it("handles 256-color cube", () => {
    // n=196 -> i=180 -> r=5,g=0,b=0 -> #ff0000
    expect(ansiToHtml("\x1b[38;5;196mred\x1b[0m")).toBe(
      '<span style="color:#ff0000">red</span>',
    );
  });

  it("handles truecolor fg", () => {
    expect(ansiToHtml("\x1b[38;2;255;0;128mpink\x1b[0m")).toBe(
      '<span style="color:rgb(255,0,128)">pink</span>',
    );
  });

  it("compounds bold + fg in one sequence", () => {
    expect(ansiToHtml("\x1b[1;31merr\x1b[0m")).toBe(
      '<span style="color:#cd3131;font-weight:bold">err</span>',
    );
  });

  it("closes the span on reset so following text is unstyled", () => {
    expect(ansiToHtml("\x1b[32mok\x1b[0m done")).toBe(
      '<span style="color:#0dbc79">ok</span> done',
    );
  });

  it("treats empty code list (\\x1b[m) as reset", () => {
    expect(ansiToHtml("\x1b[32mok\x1b[m tail")).toBe(
      '<span style="color:#0dbc79">ok</span> tail',
    );
  });

  it("supports dim as opacity", () => {
    expect(ansiToHtml("\x1b[2mfaint\x1b[22m")).toBe(
      '<span style="opacity:0.7">faint</span>',
    );
  });

  it("handles the real deno test output shape", () => {
    // Mimics: "  \x1b[38;5;245m(1ms)\x1b[0m\n"
    const out = ansiToHtml("  \x1b[38;5;245m(1ms)\x1b[0m\n");
    expect(out).toBe('  <span style="color:#8a8a8a">(1ms)</span>\n');
    // And the stripped preview (via stripAnsi, not ansiToHtml) would not
    // contain the bracket fragments — the hasAnsi discriminator covers that.
    expect(hasAnsi("  \x1b[38;5;245m(1ms)\x1b[0m\n")).toBe(true);
  });

  it("does not leak style across multiple resets", () => {
    expect(ansiToHtml("\x1b[32ma\x1b[0mb\x1b[31mc\x1b[0m")).toBe(
      '<span style="color:#0dbc79">a</span>b<span style="color:#cd3131">c</span>',
    );
  });
});
