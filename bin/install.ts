#!/usr/bin/env -S deno run -A

const REPO = "babarot/agent-recall";
const INSTALL_DIR = `${Deno.env.get("HOME")}/.claude`;

async function main(): Promise<void> {
  const os = detectOS();
  const arch = detectArch();
  const tag = await latestTag();

  console.log(`Installing agent-recall ${tag} (${os}/${arch})...`);

  await Deno.mkdir(INSTALL_DIR, { recursive: true });
  await downloadAndVerify("agent-recall", tag, os, arch);
  await Deno.chmod(`${INSTALL_DIR}/agent-recall`, 0o755);

  console.log("\nImporting existing sessions...");
  const importCmd = new Deno.Command(`${INSTALL_DIR}/agent-recall`, {
    args: ["import"],
    stdout: "inherit",
    stderr: "inherit",
  });
  await importCmd.output();

  let claudeFound = false;
  try {
    const which = new Deno.Command("which", { args: ["claude"], stdout: "null", stderr: "null" });
    const { success } = await which.output();
    claudeFound = success;
  } catch {
    // claude CLI not found
  }

  if (claudeFound) {
    console.log("Registering MCP server...");
    try {
      const mcp = new Deno.Command("claude", {
        args: ["mcp", "add", "agent-recall", "-s", "user", "--", `${INSTALL_DIR}/agent-recall`, "mcp"],
        stdout: "inherit",
        stderr: "inherit",
      });
      await mcp.output();
      console.log("  OK");
    } catch {
      console.log("  Failed (register manually)");
    }
  }

  console.log(`\nInstalled to:\n  ${INSTALL_DIR}/agent-recall`);

  if (!claudeFound) {
    console.log(`
Manual setup needed:
  Register MCP server:
  claude mcp add agent-recall -s user -- ${INSTALL_DIR}/agent-recall mcp`);
  }
}

function detectOS(): string {
  switch (Deno.build.os) {
    case "darwin":
      return "darwin";
    case "linux":
      return "linux";
    default:
      throw new Error(`Unsupported OS: ${Deno.build.os}`);
  }
}

function detectArch(): string {
  switch (Deno.build.arch) {
    case "aarch64":
      return "arm64";
    case "x86_64":
      return "x86_64";
    default:
      throw new Error(`Unsupported architecture: ${Deno.build.arch}`);
  }
}

async function latestTag(): Promise<string> {
  const resp = await fetch(
    `https://api.github.com/repos/${REPO}/releases/latest`
  );
  if (!resp.ok) {
    throw new Error(`Failed to fetch latest release: ${resp.statusText}`);
  }
  const data = await resp.json();
  return data.tag_name;
}

async function downloadAndVerify(
  name: string,
  tag: string,
  os: string,
  arch: string
): Promise<void> {
  const assetName = `${name}-${os}-${arch}`;
  const url = `https://github.com/${REPO}/releases/download/${tag}/${assetName}`;
  const checksumsUrl = `https://github.com/${REPO}/releases/download/${tag}/checksums.txt`;

  console.log(`Downloading ${assetName}...`);
  const resp = await fetch(url);
  if (!resp.ok) {
    throw new Error(`Failed to download ${assetName}: ${resp.statusText}`);
  }
  const binary = new Uint8Array(await resp.arrayBuffer());
  await Deno.writeFile(`${INSTALL_DIR}/${name}`, binary);

  console.log("Verifying checksum...");
  const checksumsResp = await fetch(checksumsUrl);
  if (!checksumsResp.ok) {
    throw new Error("Failed to download checksums");
  }
  const checksums = await checksumsResp.text();
  const expectedLine = checksums
    .split("\n")
    .find((line) => line.endsWith(`  ${assetName}`));
  if (!expectedLine) {
    throw new Error(`Checksum not found for ${assetName}`);
  }
  const expected = expectedLine.trim().split(/\s+/)[0];

  const hash = await crypto.subtle.digest("SHA-256", binary);
  const actual = Array.from(new Uint8Array(hash))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");

  if (expected !== actual) {
    await Deno.remove(`${INSTALL_DIR}/${name}`);
    throw new Error(
      `Checksum mismatch for ${name}!\n  Expected: ${expected}\n  Actual:   ${actual}`
    );
  }
  console.log("  OK");
}

main();
