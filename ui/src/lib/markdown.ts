import { marked } from "marked";
import DOMPurify from "dompurify";

// Configure marked once at module level
marked.setOptions({ breaks: true, gfm: true });

/** Parse markdown to sanitized HTML, with table wrapper for scrollable tables */
export function renderMarkdown(content: string): string {
  const raw = marked.parse(content) as string;
  const wrapped = raw
    .replace(/<table>/g, '<div class="table-wrapper"><table>')
    .replace(/<\/table>/g, "</table></div>");
  return DOMPurify.sanitize(wrapped, {
    ADD_TAGS: ["div"],
    ADD_ATTR: ["class"],
  });
}
