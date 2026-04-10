#!/usr/bin/env bash
set -euo pipefail

REPO="babarot/agent-recall"
INSTALL_DIR="${HOME}/.claude"

main() {
  local os arch tag

  os=$(detect_os)
  arch=$(detect_arch)
  tag=$(latest_tag)

  echo "Installing agent-recall ${tag} (${os}/${arch})..."

  mkdir -p "${INSTALL_DIR}"

  download_and_verify "agent-recall" "${tag}" "${os}" "${arch}"

  chmod +x "${INSTALL_DIR}/agent-recall"

  echo ""
  echo "Installed to:"
  echo "  ${INSTALL_DIR}/agent-recall"
  echo ""
  echo "Next steps:"
  echo "  1. Import existing sessions:"
  echo "     ${INSTALL_DIR}/agent-recall import"
  echo ""
  echo "  2. Set up auto-archive hook (add to ~/.claude/settings.json):"
  echo '     "hooks": { "SessionEnd": [{ "hooks": [{ "type": "command", "command": "$HOME/.claude/agent-recall import 2>/dev/null", "async": true }] }] }'
  echo ""
  echo "  3. Register MCP server:"
  echo "     claude mcp add agent-recall -- ${INSTALL_DIR}/agent-recall mcp"
}

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux)  echo "linux" ;;
    *)
      echo "Unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    arm64|aarch64) echo "arm64" ;;
    x86_64|amd64)  echo "x86_64" ;;
    *)
      echo "Unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

latest_tag() {
  local tag
  tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
  if [[ -z "${tag}" ]]; then
    echo "Failed to fetch latest release tag" >&2
    exit 1
  fi
  echo "${tag}"
}

download_and_verify() {
  local name="$1" tag="$2" os="$3" arch="$4"
  local asset_name="${name}-${os}-${arch}"
  local url="https://github.com/${REPO}/releases/download/${tag}/${asset_name}"
  local checksums_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"

  echo "Downloading ${asset_name}..."
  curl -fsSL -o "${INSTALL_DIR}/${name}" "${url}"

  echo "Verifying checksum..."
  local expected actual
  expected=$(curl -fsSL "${checksums_url}" | grep "${asset_name}" | awk '{print $1}')
  actual=$(sha256sum "${INSTALL_DIR}/${name}" 2>/dev/null || shasum -a 256 "${INSTALL_DIR}/${name}" | awk '{print $1}')

  if [[ "${expected}" != "${actual}" ]]; then
    echo "Checksum mismatch for ${name}!" >&2
    echo "  Expected: ${expected}" >&2
    echo "  Actual:   ${actual}" >&2
    rm -f "${INSTALL_DIR}/${name}"
    exit 1
  fi
  echo "  OK"
}

main "$@"
