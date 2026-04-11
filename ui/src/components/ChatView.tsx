import { useState, useEffect, useRef, useMemo } from "preact/hooks";
import { groupMessages, renderImages, stripAnsi, getToolInputPreview } from "../lib/chat-utils";
import type { Message, DisplayMessage } from "../lib/chat-utils";
import type { Settings } from "../lib/settings";
import { renderMarkdown } from "../lib/markdown";
import { ansiToHtml, hasAnsi } from "../lib/ansi";
import { useKeyboardShortcut } from "../hooks/use-keyboard-shortcut";
import { useSSE } from "../hooks/use-sse";
import { useTailFollow } from "../hooks/use-tail-follow";

interface SessionData {
  session: {
    sessionId: string;
    project: string;
    branch: string;
    date: string;
    summary: string | null;
  };
  messages: Message[];
}

function filterMessages(messages: DisplayMessage[], settings: Settings): DisplayMessage[] {
  return messages.filter((msg) => {
    if (msg.type === "thinking" && !settings.showThinking) return false;
    if (msg.type === "tool_use" && !settings.showToolUse) return false;
    if (msg.type === "tool_result" && !settings.showToolResult) return false;
    if (msg.type === "meta" && !settings.showMeta) return false;
    return true;
  });
}

const MESSAGE_RENDERERS: Record<string, (msg: DisplayMessage, i: number, sessionId: string) => preact.JSX.Element | null> = {
  bash: (msg, i) => msg.type === "bash" ? <BashBubble key={i} command={msg.command} stdout={msg.stdout} stderr={msg.stderr} /> : null,
  command: (msg, i) => msg.type === "command" ? <CommandBubble key={i} name={msg.name} args={msg.args} stdout={msg.stdout} /> : null,
  thinking: (msg, i) => msg.type === "thinking" ? <ThinkingBubble key={i} content={msg.content} /> : null,
  tool_use: (msg, i) => msg.type === "tool_use" ? <ToolUseBubble key={i} toolName={msg.toolName} toolInput={msg.toolInput} /> : null,
  tool_result: (msg, i) => msg.type === "tool_result" ? <ToolResultBubble key={i} content={msg.content} /> : null,
  meta: (msg, i) => msg.type === "meta" ? <MetaBubble key={i} label={msg.label} content={msg.content} /> : null,
  chat: (msg, i, sessionId) => msg.type === "chat" ? <ChatBubble key={i} sessionId={sessionId} uuid={msg.uuid} role={msg.role} content={msg.content} /> : null,
};

export function ChatView({ sessionId, onBack, settings }: { sessionId: string; onBack: () => void; settings: Settings }) {
  const [data, setData] = useState<SessionData | null>(null);
  const [copied, setCopied] = useState(false);
  const [zoomImage, setZoomImage] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  // Tail-follow machinery: handles "stick to bottom on live updates",
  // cancels on user scroll-up, survives image layout shifts, and guards
  // against out-of-order fetch responses.
  const { markIfAtBottom, isCurrentSeq } = useTailFollow(scrollRef, data);

  useKeyboardShortcut("Escape", () => setZoomImage(null));

  useEffect(() => {
    fetch(`/api/sessions/${sessionId}`)
      .then((r) => r.json())
      .then((d) => {
        setData(d);
        setTimeout(() => scrollRef.current?.scrollTo(0, 0), 0);
      });
  }, [sessionId]);

  // Live refresh on SSE events for the current session. We mark the
  // "was at bottom" state *before* firing the fetch so the post-render
  // effect in useTailFollow knows whether to scroll back down, and we
  // drop the response if a newer fetch has been issued in the meantime.
  useSSE((event) => {
    if (event.type !== "session_updated") return;
    if (event.sessionId !== sessionId) return;

    const seq = markIfAtBottom();
    fetch(`/api/sessions/${sessionId}`)
      .then((r) => r.json())
      .then((d) => {
        if (!isCurrentSeq(seq)) return;
        setData(d);
      })
      .catch(() => {
        // Transient fetch failure — next event will retry.
      });
  });

  const copyId = () => {
    navigator.clipboard.writeText(sessionId);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  if (!data) {
    return <div class="flex items-center justify-center h-full text-text-secondary">Loading...</div>;
  }

  if (!data.session) {
    return <div class="flex items-center justify-center h-full text-text-secondary">Session not found.</div>;
  }

  const visibleMessages = filterMessages(groupMessages(data.messages), settings);

  return (
    <div class="h-full flex flex-col">
      {/* Session header */}
      <div class="p-4 border-b border-border bg-bg-secondary">
        <div class="max-w-3xl mx-auto">
          <div class="flex items-center gap-3 mb-2">
            <button
              onClick={onBack}
              class="text-sm text-text-secondary hover:text-text transition-colors cursor-pointer"
            >
              &larr; Back
            </button>
          </div>
          <div class="flex items-center gap-3 flex-wrap">
            <button
              onClick={copyId}
              class="font-mono text-sm text-accent hover:text-accent-hover transition-colors cursor-pointer"
              title="Click to copy full session ID"
            >
              {copied ? "Copied!" : sessionId.slice(0, 8)}
            </button>
            <span class="text-sm text-text-secondary">{data.session.project}</span>
            {data.session.branch && (
              <span class="text-xs px-2 py-0.5 bg-bg-tertiary rounded text-text-muted">
                {data.session.branch}
              </span>
            )}
            <span class="text-xs text-text-muted">{data.session.date}</span>
          </div>
          {data.session.summary && (
            <p class="mt-2 text-sm text-text-secondary">{data.session.summary}</p>
          )}
        </div>
      </div>

      {/* Chat messages */}
      <div
        ref={scrollRef}
        class="flex-1 overflow-y-auto p-4"
        onClick={(e) => {
          const target = e.target as HTMLElement;
          if (target.tagName === "IMG" && target.closest(".markdown-content")) {
            setZoomImage((target as HTMLImageElement).src);
          }
        }}
      >
        <div class="max-w-3xl mx-auto space-y-4">
          {visibleMessages.map((msg, i) => {
            const renderer = MESSAGE_RENDERERS[msg.type];
            return renderer ? renderer(msg, i, data.session.sessionId) : null;
          })}
        </div>
      </div>

      {/* Image zoom overlay */}
      {zoomImage && (
        <div class="image-overlay" onClick={() => setZoomImage(null)}>
          <img src={zoomImage} alt="Zoomed" />
        </div>
      )}
    </div>
  );
}

function MetaBubble({ label, content }: { label: string; content: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div class="flex justify-center">
      <div class="max-w-[90%] w-full">
        <button
          onClick={() => setOpen(!open)}
          class="w-full flex items-center gap-2 text-xs text-text-muted hover:text-text-secondary transition-colors cursor-pointer py-1 px-3 border border-dashed border-border rounded"
          title="Meta message (synthetic content injected by Claude Code)"
        >
          <span class="text-text-muted">{open ? "▼" : "▶"}</span>
          <span class="uppercase tracking-wider text-[10px] px-1.5 py-0.5 bg-bg-tertiary rounded text-text-muted shrink-0">
            meta
          </span>
          <span class="truncate text-text-secondary">{label}</span>
        </button>
        {open && (
          <div class="mt-1 px-4 py-3 bg-bg-secondary border border-dashed border-border rounded text-xs text-text-secondary whitespace-pre-wrap leading-relaxed max-h-96 overflow-y-auto">
            {content}
          </div>
        )}
      </div>
    </div>
  );
}

function ThinkingBubble({ content }: { content: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div class="flex justify-start">
      <div class="max-w-[85%]">
        <button
          onClick={() => setOpen(!open)}
          class="flex items-center gap-2 text-xs text-text-muted hover:text-text-secondary transition-colors cursor-pointer py-1"
        >
          <span class="text-text-muted">{open ? "▼" : "▶"}</span>
          Thinking...
        </button>
        {open && (
          <div class="mt-1 px-4 py-3 bg-bg-secondary border border-border rounded-2xl rounded-bl-sm text-xs text-text-secondary whitespace-pre-wrap leading-relaxed">
            {content}
          </div>
        )}
      </div>
    </div>
  );
}

function ToolUseBubble({ toolName, toolInput }: { toolName: string; toolInput: string }) {
  const [open, setOpen] = useState(false);
  const inputPreview = getToolInputPreview(toolInput);

  return (
    <div class="flex justify-start">
      <div class="max-w-[85%]">
        <button
          onClick={() => setOpen(!open)}
          class="flex items-center gap-2 text-xs py-1 cursor-pointer"
        >
          <span class="text-text-muted">{open ? "▼" : "▶"}</span>
          <span class="px-2 py-0.5 bg-bg-tertiary border border-border rounded text-accent font-mono">{toolName}</span>
          {inputPreview && <span class="text-text-muted truncate max-w-xs">{inputPreview}</span>}
        </button>
        {open && toolInput && (
          <div class="mt-1">
            <pre class="!p-3 !m-0 !rounded-2xl !rounded-bl-sm text-xs overflow-x-auto"><code>{JSON.stringify(JSON.parse(toolInput), null, 2)}</code></pre>
          </div>
        )}
      </div>
    </div>
  );
}

function ToolResultBubble({ content }: { content: string }) {
  const [open, setOpen] = useState(false);
  if (!content) return null;
  const ansi = hasAnsi(content);
  const previewSrc = ansi ? stripAnsi(content) : content;
  const preview = previewSrc.length > 80 ? previewSrc.slice(0, 80) + "..." : previewSrc;
  const html = useMemo(() => {
    if (!open) return "";
    return ansi ? ansiToHtml(content) : renderMarkdown(content);
  }, [content, open, ansi]);

  return (
    <div class="flex justify-start">
      <div class="max-w-[85%]">
        <button
          onClick={() => setOpen(!open)}
          class="flex items-center gap-2 text-xs text-text-muted hover:text-text-secondary transition-colors cursor-pointer py-1"
        >
          <span>{open ? "▼" : "▶"}</span>
          <span class="text-text-muted">Result:</span>
          {!open && <span class="truncate max-w-md">{preview}</span>}
        </button>
        {open && (
          <div class="mt-1 px-4 py-3 bg-bg-secondary border border-border rounded-2xl rounded-bl-sm text-sm leading-relaxed">
            {ansi ? (
              <pre
                class="!p-0 !m-0 !border-0 text-xs text-text-secondary whitespace-pre-wrap break-words font-mono"
                dangerouslySetInnerHTML={{ __html: html }}
              />
            ) : (
              <div class="markdown-content break-words" dangerouslySetInnerHTML={{ __html: html }} />
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function BashBubble({ command, stdout, stderr }: { command: string; stdout: string; stderr: string }) {
  return (
    <div class="flex justify-end">
      <div class="max-w-[85%] rounded-2xl border border-border overflow-hidden rounded-br-sm">
        <div class="px-4 py-2 bg-bg-tertiary border-b border-border">
          <span class="text-xs font-medium text-text-muted">Shell</span>
          <pre class="mt-1 !p-2 !m-0 !border-0 !rounded text-sm"><code>$ {command}</code></pre>
        </div>
        {(stdout || stderr) && (
          <div class="px-4 py-2 bg-bg-secondary">
            <pre class="!p-2 !m-0 !border-0 !rounded text-xs text-text-secondary whitespace-pre-wrap"><code>{stripAnsi(stdout || stderr)}</code></pre>
          </div>
        )}
      </div>
    </div>
  );
}

function CommandBubble({ name, args, stdout }: { name: string; args: string; stdout: string }) {
  return (
    <div class="flex justify-end">
      <div class="max-w-[85%] rounded-2xl border border-border overflow-hidden rounded-br-sm">
        <div class="px-4 py-2 bg-bg-tertiary border-b border-border">
          <span class="text-xs font-medium text-text-muted">Command</span>
          <div class="mt-1 text-sm font-mono text-accent">{name}{args ? ` ${args}` : ""}</div>
        </div>
        {stdout && (
          <div class="px-4 py-2 bg-bg-secondary">
            <pre class="!p-2 !m-0 !border-0 !rounded text-xs text-text-secondary whitespace-pre-wrap"><code>{stripAnsi(stdout)}</code></pre>
          </div>
        )}
      </div>
    </div>
  );
}

function ChatBubble({ sessionId, uuid, role, content }: { sessionId: string; uuid: string; role: string; content: string }) {
  const isUser = role === "user";
  const html = useMemo(() => {
    const withImages = renderImages(content, sessionId, uuid);
    return renderMarkdown(withImages);
  }, [content, sessionId, uuid]);

  return (
    <div class={`flex ${isUser ? "justify-end" : "justify-start"}`}>
      <div
        class={`max-w-[85%] rounded-2xl px-4 py-3 text-sm leading-relaxed ${
          isUser
            ? "bg-user-bubble text-text border border-border rounded-br-sm"
            : "bg-assistant-bubble text-text border border-border rounded-bl-sm"
        }`}
      >
        <div class="mb-1">
          <span class={`text-xs font-medium ${isUser ? "text-accent" : "text-text-muted"}`}>
            {isUser ? "You" : "Assistant"}
          </span>
        </div>
        <div class="markdown-content break-words" dangerouslySetInnerHTML={{ __html: html }} />
      </div>
    </div>
  );
}
