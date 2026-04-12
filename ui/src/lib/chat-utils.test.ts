import { describe, it, expect } from "vitest";
import {
  extractTag,
  groupMessages,
  stripAnsi,
  renderImages,
  getToolInputPreview,
  summarizeMeta,
} from "./chat-utils";
import type { Message } from "./chat-utils";

function msg(uuid: string, role: string, content: string, blockType?: string): Message {
  return { uuid, role, content, blockType };
}

// --- extractTag ---

describe("extractTag", () => {
  it("extracts content from a simple tag", () => {
    expect(extractTag("<bash-input>git push</bash-input>", "bash-input")).toBe("git push");
  });

  it("returns null when tag not found", () => {
    expect(extractTag("no tags here", "bash-input")).toBe(null);
  });

  it("extracts multiline content", () => {
    const content = "<bash-stdout>line1\nline2\nline3</bash-stdout>";
    expect(extractTag(content, "bash-stdout")).toBe("line1\nline2\nline3");
  });

  it("trims whitespace", () => {
    expect(extractTag("<command-name>  /commit  </command-name>", "command-name")).toBe("/commit");
  });
});

// --- stripAnsi ---

describe("stripAnsi", () => {
  it("removes ANSI escape codes", () => {
    expect(stripAnsi("\x1b[1mBold\x1b[0m")).toBe("Bold");
    expect(stripAnsi("\x1b[31mRed\x1b[0m")).toBe("Red");
  });

  it("removes bracket-style escape codes", () => {
    expect(stripAnsi("^[[1mBold^[[22m")).toBe("Bold");
  });

  it("returns plain text unchanged", () => {
    expect(stripAnsi("hello world")).toBe("hello world");
  });
});

// --- renderImages ---

describe("renderImages", () => {
  it("replaces [Image #1] with API URL", () => {
    const result = renderImages("[Image #1] check this", "sess1", "msg1");
    expect(result).toBe("![Image](/api/image?session=sess1&message=msg1&index=0) check this");
  });

  it("replaces multiple [Image #N] with sequential indexes", () => {
    const result = renderImages("[Image #1] and [Image #2]", "s1", "m1");
    expect(result).toContain("index=0");
    expect(result).toContain("index=1");
  });

  it("replaces [Image: source: /path] with file API URL", () => {
    const result = renderImages("[Image: source: /Users/bob/photo.png]", "s1", "m1");
    expect(result).toContain("/api/file?path=");
    expect(result).toContain("photo.png");
  });

  it("handles both patterns in same content", () => {
    const content = "[Image #1] and [Image: source: /tmp/x.png]";
    const result = renderImages(content, "s1", "m1");
    expect(result).toContain("/api/image?");
    expect(result).toContain("/api/file?");
  });

  it("returns content unchanged when no images", () => {
    expect(renderImages("no images", "s1", "m1")).toBe("no images");
  });
});

// --- groupMessages ---

describe("groupMessages", () => {
  it("passes through regular chat messages", () => {
    const messages = [
      msg("u1", "user", "hello"),
      msg("a1", "assistant", "hi"),
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(2);
    expect(result[0].type).toBe("chat");
    expect(result[1].type).toBe("chat");
  });

  it("groups caveat + bash-input + bash-stdout into bash bubble", () => {
    const messages = [
      msg("u1", "user", "<local-command-caveat>Caveat: ...</local-command-caveat>"),
      msg("u2", "user", "<bash-input>git push</bash-input>"),
      msg("u3", "user", "<bash-stdout>Everything up-to-date</bash-stdout><bash-stderr></bash-stderr>"),
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("bash");
    if (result[0].type === "bash") {
      expect(result[0].command).toBe("git push");
      expect(result[0].stdout).toBe("Everything up-to-date");
    }
  });

  it("groups caveat + command-name + local-command-stdout into command bubble", () => {
    const messages = [
      msg("u1", "user", "<local-command-caveat>Caveat: ...</local-command-caveat>"),
      msg("u2", "user", "<command-name>/add-dir</command-name>\n<command-args>/Users/bob/project</command-args>"),
      msg("u3", "user", "<local-command-stdout>Added /Users/bob/project</local-command-stdout>"),
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("command");
    if (result[0].type === "command") {
      expect(result[0].name).toBe("/add-dir");
      expect(result[0].args).toBe("/Users/bob/project");
      expect(result[0].stdout).toBe("Added /Users/bob/project");
    }
  });

  it("handles standalone bash-input without caveat", () => {
    const messages = [
      msg("u1", "user", "<bash-input>ls -la</bash-input>"),
      msg("u2", "user", "<bash-stdout>total 0</bash-stdout><bash-stderr></bash-stderr>"),
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("bash");
    if (result[0].type === "bash") {
      expect(result[0].command).toBe("ls -la");
    }
  });

  it("skips standalone stdout/stderr messages", () => {
    const messages = [
      msg("u1", "user", "hello"),
      msg("u2", "user", "<bash-stdout>orphaned output</bash-stdout>"),
      msg("a1", "assistant", "hi"),
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(2);
    expect(result[0].type).toBe("chat");
    expect(result[1].type).toBe("chat");
  });

  it("skips image-source-only messages", () => {
    const messages = [
      msg("u1", "user", "[Image #1] check this"),
      msg("u2", "user", "[Image: source: /Users/bob/photo.png]"),
      msg("a1", "assistant", "I see it"),
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(2);
    expect(result[0].type).toBe("chat");
    if (result[0].type === "chat") {
      expect(result[0].content).toContain("[Image #1]");
    }
    expect(result[1].type).toBe("chat");
  });

  it("does not skip image-source messages with additional text", () => {
    const messages = [
      msg("u1", "user", "[Image: source: /tmp/x.png] what is this?"),
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("chat");
  });

  it("handles mixed regular and bash messages", () => {
    const messages = [
      msg("u1", "user", "do something"),
      msg("a1", "assistant", "ok"),
      msg("u2", "user", "<local-command-caveat>Caveat</local-command-caveat>"),
      msg("u3", "user", "<bash-input>make build</bash-input>"),
      msg("u4", "user", "<bash-stdout>done</bash-stdout><bash-stderr></bash-stderr>"),
      msg("u5", "user", "looks good"),
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(4);
    expect(result[0].type).toBe("chat");
    expect(result[1].type).toBe("chat");
    expect(result[2].type).toBe("bash");
    expect(result[3].type).toBe("chat");
  });

  it("handles empty message list", () => {
    expect(groupMessages([])).toHaveLength(0);
  });

  it("maps thinking blockType to thinking display type", () => {
    const messages: Message[] = [
      { uuid: "u1", role: "assistant", blockType: "thinking", content: "reasoning..." },
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("thinking");
    if (result[0].type === "thinking") {
      expect(result[0].content).toBe("reasoning...");
    }
  });

  it("maps tool_use blockType to tool_use display type", () => {
    const messages: Message[] = [
      { uuid: "u1", role: "assistant", blockType: "tool_use", content: "Bash", toolName: "Bash", toolInput: '{"command":"ls"}' },
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("tool_use");
    if (result[0].type === "tool_use") {
      expect(result[0].toolName).toBe("Bash");
    }
  });

  it("maps tool_result blockType to tool_result display type", () => {
    const messages: Message[] = [
      { uuid: "u1", role: "user", blockType: "tool_result", content: "output here" },
    ];
    const result = groupMessages(messages);
    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("tool_result");
  });
});

// --- getToolInputPreview ---

describe("getToolInputPreview", () => {
  it("extracts command field", () => {
    expect(getToolInputPreview('{"command":"git push"}')).toBe("git push");
  });

  it("extracts file_path field", () => {
    expect(getToolInputPreview('{"file_path":"/tmp/x.ts"}')).toBe("/tmp/x.ts");
  });

  it("extracts query field", () => {
    expect(getToolInputPreview('{"query":"terraform"}')).toBe("terraform");
  });

  it("extracts url field", () => {
    expect(getToolInputPreview('{"url":"https://example.com"}')).toBe("https://example.com");
  });

  it("returns empty string for unknown fields", () => {
    expect(getToolInputPreview('{"foo":"bar"}')).toBe("");
  });

  it("returns empty string for invalid JSON", () => {
    expect(getToolInputPreview("not json")).toBe("");
  });

  it("returns empty string for empty input", () => {
    expect(getToolInputPreview("")).toBe("");
  });
});

// --- summarizeMeta ---

describe("summarizeMeta", () => {
  it("labels skill expansions with the skill name", () => {
    const content = "Base directory for this skill: /Users/foo/.claude/plugins/marketplaces/bar/skills/open-pr\n\n# Open PR";
    expect(summarizeMeta(content)).toBe("Skill: open-pr");
  });

  it("labels ## Context injections", () => {
    expect(summarizeMeta("## Context\n\n- Current git status: ...")).toBe("Injected context");
  });

  it("labels command caveats", () => {
    expect(summarizeMeta("<local-command-caveat>Caveat: ...</local-command-caveat>")).toBe("Command caveat");
  });

  it("falls back to the first non-empty line truncated", () => {
    const content = "Something unexpected happened\nsecond line\nthird line";
    expect(summarizeMeta(content)).toBe("Something unexpected happened");
  });

  it("strips leading markdown heading markers", () => {
    expect(summarizeMeta("### Some header\nbody")).toBe("Some header");
  });

  it("truncates very long single-line fallbacks", () => {
    const long = "a".repeat(200);
    const result = summarizeMeta(long);
    expect(result.endsWith("…")).toBe(true);
    expect(result.length).toBeLessThanOrEqual(61);
  });

  it("returns 'Meta message' for empty input", () => {
    expect(summarizeMeta("")).toBe("Meta message");
  });
});

// --- groupMessages meta handling ---

describe("groupMessages meta handling", () => {
  it("emits a meta display message for blockType=meta", () => {
    const result = groupMessages([
      msg("u1", "user", "real human text"),
      msg(
        "m1",
        "user",
        "Base directory for this skill: /.../skills/open-pr\n\n# Open PR Skill",
        "meta"
      ),
      msg("a1", "assistant", "ok"),
    ]);

    expect(result).toHaveLength(3);
    expect(result[0].type).toBe("chat");
    expect(result[1].type).toBe("meta");
    if (result[1].type === "meta") {
      expect(result[1].label).toBe("Skill: open-pr");
      expect(result[1].content.startsWith("Base directory")).toBe(true);
    }
    expect(result[2].type).toBe("chat");
  });

  it("meta display does not swallow an adjacent command bubble", () => {
    // A <command-name> message is NOT isMeta in the real JSONL, but this
    // test just verifies that when we have both on-screen they render as
    // separate display messages.
    const result = groupMessages([
      msg("c1", "user", "<command-message>commit</command-message> <command-name>/commit</command-name>"),
      msg("m1", "user", "## Context\n...", "meta"),
    ]);

    expect(result).toHaveLength(2);
    expect(result[0].type).toBe("command");
    expect(result[1].type).toBe("meta");
    if (result[1].type === "meta") {
      expect(result[1].label).toBe("Injected context");
    }
  });
});

// --- False-positive guards for content-based tag detection ---
//
// These regressions come from assistant explanations that mention command
// tags verbatim (e.g. "the `<command-name>` tag…"). Before the guard was
// added, those produced empty Command bubbles because extractTag failed to
// find a closing tag inside the inline mention.

describe("groupMessages false-positive guard", () => {
  it("does not misclassify an assistant text that mentions <command-name>", () => {
    const result = groupMessages([
      msg(
        "a1",
        "assistant",
        "The `<command-name>` tag is what Claude Code emits when you invoke a slash command.",
        "text"
      ),
    ]);

    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("chat");
    if (result[0].type === "chat") {
      expect(result[0].role).toBe("assistant");
    }
  });

  it("does not misclassify an assistant text that mentions <bash-input>", () => {
    const result = groupMessages([
      msg(
        "a1",
        "assistant",
        "Claude Code wraps shell commands in `<bash-input>...</bash-input>`.",
        "text"
      ),
    ]);

    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("chat");
  });

  it("does not misclassify an assistant text that mentions <local-command-caveat>", () => {
    const result = groupMessages([
      msg(
        "a1",
        "assistant",
        "Bash caveats arrive in a `<local-command-caveat>` wrapper.",
        "text"
      ),
    ]);

    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("chat");
  });

  it("does not hide a tool_result whose grep output contains <command-name>", () => {
    const result = groupMessages([
      msg(
        "t1",
        "user",
        'grep hit: <command-name>foo</command-name> found in file.jsonl',
        "tool_result"
      ),
    ]);

    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("tool_result");
  });

  it("still recognizes a real user <command-name> message", () => {
    const result = groupMessages([
      msg(
        "c1",
        "user",
        "<command-message>open-pr</command-message> <command-name>/core:open-pr</command-name> <command-args>英語で</command-args>",
        "text"
      ),
    ]);

    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("command");
    if (result[0].type === "command") {
      expect(result[0].name).toBe("/core:open-pr");
      expect(result[0].args).toBe("英語で");
    }
  });
});
