import { marked } from "marked";
import DOMPurify from "dompurify";

// Configure marked once at module level
marked.setOptions({ breaks: true, gfm: true });

// --- wrapIndentedBlocks --------------------------------------------------
//
// Preprocessor: any contiguous run of lines where every line starts with
// 2+ spaces and the run begins at a paragraph boundary (previous line
// blank, or document start) is wrapped in a ``` fenced code block. This
// preserves the author's intended layout instead of letting marked reflow
// it as a paragraph/list.
//
// Content already inside an existing fenced code block (``` / ~~~) is
// passed through untouched. Fence matching follows CommonMark §4.5:
// closers must be the same character and have a length ≥ the opener's,
// and closers must not carry an info-string.

const FENCE_RE = /^ {0,3}(`{3,}|~{3,})(.*)$/;
const INDENTED_RE = /^ {2,}\S/;

interface FenceDesc {
  char: "`" | "~";
  len: number;
  /** true iff nothing follows the fence run except whitespace */
  closerCandidate: boolean;
}

function parseFence(line: string): FenceDesc | null {
  const m = line.match(FENCE_RE);
  if (!m) return null;
  const run = m[1];
  const tail = m[2];
  return {
    char: run[0] as "`" | "~",
    len: run.length,
    closerCandidate: tail.trim() === "",
  };
}

function chooseFence(block: string[]): string {
  let maxRun = 2;
  for (const line of block) {
    const matches = line.match(/`+/g);
    if (!matches) continue;
    for (const run of matches) {
      if (run.length > maxRun) maxRun = run.length;
    }
  }
  return "`".repeat(maxRun + 1);
}

export function wrapIndentedBlocks(content: string): string {
  if (!content) return content;

  const lines = content.split("\n");
  const out: string[] = [];
  let fenceChar: "`" | "~" | null = null;
  let fenceLen = 0;
  let prevBlank = true; // document start counts as a paragraph boundary
  let block: string[] = [];

  const flushBlock = () => {
    if (block.length === 0) return;
    // Strip the common leading indent. All lines in `block` have been
    // qualified by INDENTED_RE (≥2 leading spaces), so minIndent ≥ 2.
    // Rationale: when we auto-promote a spaced block to a code fence,
    // the leading indent is a *syntactic marker* for "this is code",
    // not content — same semantics as CommonMark's 4-space indented
    // code blocks. Explicit ``` fences written by the user still keep
    // their inner whitespace verbatim because they bypass this path.
    let minIndent = Infinity;
    for (const l of block) {
      const m = l.match(/^ */);
      const indent = m ? m[0].length : 0;
      if (indent < minIndent) minIndent = indent;
    }
    const dedented = block.map((l) => l.slice(minIndent));
    const fence = chooseFence(dedented);
    out.push(fence);
    for (const l of dedented) out.push(l);
    out.push(fence);
    block = [];
  };

  for (const line of lines) {
    if (fenceChar !== null) {
      // Inside an existing fence — close only on a matching closer.
      const fence = parseFence(line);
      if (
        fence &&
        fence.char === fenceChar &&
        fence.len >= fenceLen &&
        fence.closerCandidate
      ) {
        fenceChar = null;
        fenceLen = 0;
      }
      out.push(line);
      prevBlank = line.trim() === "";
      continue;
    }

    // Outside any fence.
    const isBlank = line.trim() === "";
    const isIndented = INDENTED_RE.test(line);

    // (1) Continue an active indented block if the line still qualifies.
    if (block.length > 0 && isIndented) {
      block.push(line);
      prevBlank = false;
      continue;
    }
    // (2) End of an active block — flush and fall through.
    if (block.length > 0) {
      flushBlock();
    }

    // (3) Start a new indented block at a paragraph boundary. This takes
    //     precedence over fence-opener recognition, so indented lines
    //     that also happen to look like fence markers ("  ```js") are
    //     preserved inside the wrapped code block.
    if (isIndented && prevBlank) {
      block.push(line);
      prevBlank = false;
      continue;
    }

    // (4) Try to open a new fence.
    const fence = parseFence(line);
    if (fence) {
      fenceChar = fence.char;
      fenceLen = fence.len;
      out.push(line);
      prevBlank = false;
      continue;
    }

    // (5) Plain line.
    out.push(line);
    prevBlank = isBlank;
  }

  flushBlock();
  return out.join("\n");
}

/** Parse markdown to sanitized HTML, with table wrapper for scrollable tables */
export function renderMarkdown(content: string): string {
  const preprocessed = wrapIndentedBlocks(content);
  const raw = marked.parse(preprocessed) as string;
  const wrapped = raw
    .replace(/<table>/g, '<div class="table-wrapper"><table>')
    .replace(/<\/table>/g, "</table></div>");
  return DOMPurify.sanitize(wrapped, {
    ADD_TAGS: ["div"],
    ADD_ATTR: ["class"],
  });
}
