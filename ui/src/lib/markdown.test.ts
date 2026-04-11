import { describe, it, expect } from "vitest";
import { renderMarkdown, wrapIndentedBlocks } from "./markdown";

// We test wrapIndentedBlocks directly for the transformation contract,
// and renderMarkdown for the observable HTML shape.

describe("wrapIndentedBlocks", () => {
  it("wraps a basic indented block and strips the common indent", () => {
    const input = "  aaa\n  bbb\n  ccc";
    const out = wrapIndentedBlocks(input);
    expect(out).toBe("```\naaa\nbbb\nccc\n```");
  });

  it("preserves relative indentation inside the block", () => {
    // Minimum indent is 2 → strip 2. Deeper lines keep their extra indent.
    const input = "  aaa\n    bbb\n  ccc";
    expect(wrapIndentedBlocks(input)).toBe("```\naaa\n  bbb\nccc\n```");
  });

  it("passes through content already inside a fenced block (indent preserved)", () => {
    // Explicit ``` fences are untouched — the 2-space indent inside is
    // user-authored content and must be preserved verbatim, distinct
    // from the auto-wrap path which dedents.
    const input = "```\n  aaa\n  bbb\n```";
    expect(wrapIndentedBlocks(input)).toBe(input);
  });

  it("does not wrap list-item continuation lines", () => {
    const input = "- item1\n  continuation\n- item2";
    expect(wrapIndentedBlocks(input)).toBe(input);
  });

  it("wraps only the indented region in mixed content", () => {
    const input = "para1\n\n  code1\n  code2\n\npara2";
    expect(wrapIndentedBlocks(input)).toBe(
      "para1\n\n```\ncode1\ncode2\n```\n\npara2",
    );
  });

  it("wraps indented content at document start", () => {
    const input = "  aaa\n  bbb";
    expect(wrapIndentedBlocks(input)).toBe("```\naaa\nbbb\n```");
  });

  it("wraps a single indented line", () => {
    expect(wrapIndentedBlocks("  lonely")).toBe("```\nlonely\n```");
  });

  it("blank line terminates a block; a new block may start after", () => {
    const input = "  aaa\n\n  bbb";
    expect(wrapIndentedBlocks(input)).toBe("```\naaa\n```\n\n```\nbbb\n```");
  });

  it("does not wrap tab-indented lines", () => {
    const input = "\tlooks-like-code";
    expect(wrapIndentedBlocks(input)).toBe(input);
  });

  it("uses a longer outer fence when the block contains ``` ", () => {
    const input = "  ```js\n  const x = 1;\n  ```";
    const out = wrapIndentedBlocks(input);
    // Fence collision: block has runs up to 3 backticks → outer is 4.
    // Common 2-space indent is stripped before wrapping.
    expect(out).toBe("````\n```js\nconst x = 1;\n```\n````");
  });

  it("does not close a 4-backtick fence on an inner 3-backtick line", () => {
    // Opener is ````, inner ``` must be treated as content, not a closer.
    // As a result the whole input is still inside the existing fence
    // and the trailing indented line must NOT be re-wrapped.
    const input = "````\n  ```\n  x\n````";
    expect(wrapIndentedBlocks(input)).toBe(input);
  });

  it("does not let ~~~ close a ``` fence (and vice versa)", () => {
    const input = "```\n~~~\n  indented\n```";
    // ~~~ and the indented line are both fence content, untouched.
    expect(wrapIndentedBlocks(input)).toBe(input);
  });

  it("ignores a would-be closer that carries an info string", () => {
    // The second ``` has trailing `foo` → not a valid closer per CommonMark.
    // So we're still inside the fence, and the final indented line stays
    // inside the fence as content, not re-wrapped.
    const input = "```\n  inside\n``` foo\n  still inside\n```";
    expect(wrapIndentedBlocks(input)).toBe(input);
  });

  it("returns empty string unchanged", () => {
    expect(wrapIndentedBlocks("")).toBe("");
  });
});

describe("renderMarkdown (indented-block integration)", () => {
  it("renders an indented block as a <pre><code>", () => {
    const html = renderMarkdown("  aaa\n  bbb\n  ccc");
    expect(html).toContain("<pre>");
    expect(html).toContain("<code>");
    // Leading 2-space indent is preserved inside the code block.
    expect(html).toMatch(/<code[^>]*>\s*aaa\b/);
    expect(html).not.toContain("<ul>");
  });

  it("keeps a normal bulleted list as a list (no wrap)", () => {
    const html = renderMarkdown("- one\n- two\n- three");
    expect(html).toContain("<ul>");
    expect(html).toContain("<li>one</li>");
    // Should not have code blocks from this input.
    expect(html).not.toContain("<pre>");
  });

  it("keeps list-item continuation intact", () => {
    const html = renderMarkdown("- item1\n  continuation\n- item2");
    expect(html).toContain("<ul>");
    // marked collapses the continuation into the item content.
    expect(html).toContain("continuation");
    expect(html).not.toContain("<pre>");
  });

  it("renders a pre-existing ``` block as code without double wrapping", () => {
    const html = renderMarkdown("```\n  inside\n```");
    expect(html).toContain("<pre>");
    // Only one pre — no accidental doubling.
    expect(html.match(/<pre/g)?.length ?? 0).toBe(1);
    expect(html).toContain("inside");
  });
});
