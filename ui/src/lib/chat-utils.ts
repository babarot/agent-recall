export interface Message {
  uuid: string;
  role: string;
  blockType?: string;
  content: string;
  toolName?: string;
  toolInput?: string;
  timestamp?: string;
}

export type DisplayMessage =
  | { type: "chat"; uuid: string; role: string; content: string; timestamp?: string }
  | { type: "thinking"; content: string }
  | { type: "tool_use"; toolName: string; toolInput: string }
  | { type: "tool_result"; content: string }
  | { type: "bash"; command: string; stdout: string; stderr: string }
  | { type: "command"; name: string; args: string; stdout: string }
  | { type: "meta"; label: string; content: string };

export function extractTag(content: string, tag: string): string | null {
  const re = new RegExp(`<${tag}>([\\s\\S]*?)</${tag}>`);
  const m = content.match(re);
  return m ? m[1].trim() : null;
}

export function groupMessages(messages: Message[]): DisplayMessage[] {
  const result: DisplayMessage[] = [];
  let i = 0;

  while (i < messages.length) {
    const msg = messages[i];
    const content = msg.content;

    // The content-based tag detection below ONLY applies to real user-role
    // text messages. Assistant explanations and tool results can freely
    // contain literal strings like `<command-name>` or `<bash-input>`
    // without being an actual invocation — guard against false positives
    // like an assistant that writes "`<command-name>` is a tag Claude Code
    // emits" ending up as an empty CommandBubble.
    const canDetectUserTags =
      msg.role === "user" && (msg.blockType === "text" || !msg.blockType);

    // Detect <local-command-caveat> — skip it, look ahead for bash-input + bash-stdout
    if (canDetectUserTags && content.includes("<local-command-caveat>")) {
      let command = "";
      let stdout = "";
      let stderr = "";
      let commandName = "";
      let commandArgs = "";
      let j = i + 1;

      while (j < messages.length && j <= i + 3) {
        const next = messages[j].content;
        if (next.includes("<bash-input>")) {
          command = extractTag(next, "bash-input") ?? next;
        } else if (next.includes("<bash-stdout>") || next.includes("<bash-stderr>")) {
          stdout = extractTag(next, "bash-stdout") ?? "";
          stderr = extractTag(next, "bash-stderr") ?? "";
        } else if (next.includes("<command-name>")) {
          commandName = extractTag(next, "command-name") ?? "";
          commandArgs = extractTag(next, "command-args") ?? "";
        } else if (next.includes("<local-command-stdout>")) {
          stdout = extractTag(next, "local-command-stdout") ?? "";
        } else {
          break;
        }
        j++;
      }

      if (command) {
        result.push({ type: "bash", command, stdout, stderr });
      } else if (commandName) {
        result.push({ type: "command", name: commandName, args: commandArgs, stdout });
      }
      i = j;
      continue;
    }

    // Detect standalone bash-input (without caveat)
    if (canDetectUserTags && content.includes("<bash-input>")) {
      const command = extractTag(content, "bash-input") ?? content;
      let stdout = "";
      let stderr = "";
      if (i + 1 < messages.length) {
        const next = messages[i + 1].content;
        if (next.includes("<bash-stdout>") || next.includes("<bash-stderr>")) {
          stdout = extractTag(next, "bash-stdout") ?? "";
          stderr = extractTag(next, "bash-stderr") ?? "";
          i += 2;
          result.push({ type: "bash", command, stdout, stderr });
          continue;
        }
      }
      result.push({ type: "bash", command, stdout, stderr });
      i++;
      continue;
    }

    // Detect standalone <command-name> (slash commands)
    if (canDetectUserTags && (content.includes("<command-name>") || content.includes("<command-message>"))) {
      const name = extractTag(content, "command-name") ?? "";
      const args = extractTag(content, "command-args") ?? "";
      let stdout = "";
      if (i + 1 < messages.length) {
        const next = messages[i + 1].content;
        if (next.includes("<local-command-stdout>")) {
          stdout = extractTag(next, "local-command-stdout") ?? "";
          i += 2;
          result.push({ type: "command", name, args, stdout });
          continue;
        }
      }
      result.push({ type: "command", name, args, stdout });
      i++;
      continue;
    }

    // Skip standalone stdout/stderr messages (already consumed).
    // Same guard as above: only user-role text can be a stray
    // bash output wrapper; assistant explanations mentioning these
    // tags should stay readable.
    if (
      canDetectUserTags &&
      (content.includes("<bash-stdout>") ||
        content.includes("<bash-stderr>") ||
        content.includes("<local-command-stdout>"))
    ) {
      i++;
      continue;
    }

    // Skip image-source-only messages that duplicate the previous message's embedded image
    if (/^\s*\[Image:?\s*source:\s*[^\]]+\]\s*$/.test(content)) {
      i++;
      continue;
    }

    // Block type handling
    if (msg.blockType === "thinking") {
      result.push({ type: "thinking", content });
      i++;
      continue;
    }
    if (msg.blockType === "tool_use") {
      result.push({ type: "tool_use", toolName: msg.toolName ?? content, toolInput: msg.toolInput ?? "" });
      i++;
      continue;
    }
    if (msg.blockType === "tool_result") {
      result.push({ type: "tool_result", content });
      i++;
      continue;
    }
    if (msg.blockType === "meta") {
      result.push({ type: "meta", label: summarizeMeta(content), content });
      i++;
      continue;
    }

    // Regular text message
    result.push({ type: "chat", uuid: msg.uuid, role: msg.role, content, timestamp: msg.timestamp });
    i++;
  }

  return result;
}

/**
 * Build a short 1-line label for a collapsed meta block. Used as the
 * visible summary on the folded bubble so the user can tell what kind of
 * meta it is without having to expand it.
 */
export function summarizeMeta(content: string): string {
  const trimmed = content.trimStart();

  // Skill expansions: "Base directory for this skill: /path/to/.../skills/<name>"
  const skillMatch = trimmed.match(/^Base directory for this skill:.*\/skills\/([^/\s]+)/);
  if (skillMatch) return `Skill: ${skillMatch[1]}`;

  // /commit-commands:commit and similar that inject a "## Context" block
  if (/^##\s*Context\b/.test(trimmed)) return "Injected context";

  // Bash caveats
  if (trimmed.startsWith("<local-command-caveat>")) return "Command caveat";

  // Fallback: first non-empty line, truncated
  const firstLine = trimmed.split("\n", 1)[0] ?? "";
  const stripped = firstLine.replace(/^#+\s*/, "").trim();
  return (stripped.length > 60 ? stripped.slice(0, 60) + "…" : stripped) || "Meta message";
}

/** Extract a short preview string from tool input JSON */
export function getToolInputPreview(toolInput: string): string {
  try {
    const parsed = JSON.parse(toolInput);
    return parsed.command ?? parsed.pattern ?? parsed.file_path ?? parsed.query ?? parsed.url ?? parsed.prompt ?? "";
  } catch {
    return "";
  }
}

export function stripAnsi(text: string): string {
  return text.replace(/\x1b\[[0-9;]*m/g, "").replace(/\^?\[\[[\d;]*m/g, "");
}

export function renderImages(content: string, sessionId: string, uuid: string): string {
  // Replace [Image #N] with DB image
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
