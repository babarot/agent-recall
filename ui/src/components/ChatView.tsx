import { useState, useEffect, useRef, useMemo } from "preact/hooks";
import { marked } from "marked";

interface Message {
  uuid: string;
  role: string;
  content: string;
}

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

export function ChatView({ sessionId, onBack }: { sessionId: string; onBack: () => void }) {
  const [data, setData] = useState<SessionData | null>(null);
  const [copied, setCopied] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fetch(`/api/sessions/${sessionId}`)
      .then((r) => r.json())
      .then((d) => {
        setData(d);
        // Scroll to top on load
        setTimeout(() => scrollRef.current?.scrollTo(0, 0), 0);
      });
  }, [sessionId]);

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
      <div ref={scrollRef} class="flex-1 overflow-y-auto p-4">
        <div class="max-w-3xl mx-auto space-y-4">
          {groupMessages(data.messages).map((msg, i) =>
            msg.type === "bash" ? (
              <BashBubble key={i} command={msg.command} stdout={msg.stdout} stderr={msg.stderr} />
            ) : msg.type === "command" ? (
              <CommandBubble key={i} name={msg.name} args={msg.args} stdout={msg.stdout} />
            ) : (
              <ChatBubble key={i} sessionId={data.session.sessionId} uuid={msg.uuid} role={msg.role} content={msg.content} />
            )
          )}
        </div>
      </div>
    </div>
  );
}

function renderImages(content: string, sessionId: string, uuid: string): string {
  // Replace [Image #N] with DB image
  // N is a session-global counter, but images are stored per-message with index starting at 0
  let imgCounter = 0;
  let result = content.replace(/\[Image #\d+\]/g, () => {
    const index = imgCounter++;
    return `![Image](${`/api/image?session=${sessionId}&message=${uuid}&index=${index}`})`;
  });

  // Replace [Image: source: /path/to/file] with local file
  result = result.replace(/\[Image:?\s*source:\s*([^\]]+)\]/g, (_match, path) => {
    return `![Image](${`/api/file?path=${encodeURIComponent(path.trim())}`})`;
  });

  return result;
}

function ChatBubble({ sessionId, uuid, role, content }: { sessionId: string; uuid: string; role: string; content: string }) {
  const isUser = role === "user";
  const html = useMemo(() => {
    marked.setOptions({ breaks: true, gfm: true });
    const withImages = renderImages(content, sessionId, uuid);
    return marked.parse(withImages) as string;
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
