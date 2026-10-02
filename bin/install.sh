#!/usr/bin/env bash
# Install the latest claude-recall release: the `recall` binary, an import of
# existing sessions, and the MCP server registration.
#
#   curl -fsSL https://raw.githubusercontent.com/babarot/claude-recall/main/bin/install.sh | bash
#
# RECALL_INSTALL_DIR overrides the install directory (default: ~/.local/bin).
set -euo pipefail

REPO="babarot/claude-recall"
INSTALL_DIR="${RECALL_INSTALL_DIR:-${HOME}/.local/bin}"

main() {
  local os arch tag
  os=$(detect_os)
  arch=$(detect_arch)
  tag=$(latest_tag)

  echo "Installing claude-recall ${tag} (${os}/${arch})..."
  mkdir -p "${INSTALL_DIR}"
  download_and_verify "claude-recall-${os}-${arch}" "${tag}" "${INSTALL_DIR}/recall"
  chmod +x "${INSTALL_DIR}/recall"

  echo ""
  echo "Importing existing sessions..."
  "${INSTALL_DIR}/recall" import

  echo ""
  if command -v claude &>/dev/null; then
    echo "Registering MCP server..."
    claude mcp add claude-recall -s user -- "${INSTALL_DIR}/recall" mcp 2>/dev/null && echo "  OK" || echo "  Failed (register manually)"
  else
    echo "Register the MCP server once Claude Code is installed:"
    echo "  claude mcp add claude-recall -s user -- ${INSTALL_DIR}/recall mcp"
  fi

  echo ""
  echo "Installed to ${INSTALL_DIR}/recall"
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *) echo "Add ${INSTALL_DIR} to PATH to run recall." ;;
  esac
}

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    *) echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    arm64 | aarch64) echo "arm64" ;;
    x86_64 | amd64) echo "x86_64" ;;
    *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac
}

latest_tag() {
  local tag
  tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
  if [[ -z ${tag} ]]; then
    echo "Failed to fetch the latest release tag" >&2
    exit 1
  fi
  echo "${tag}"
}

sha256() {
  if command -v sha256sum &>/dev/null; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

download_and_verify() {
  local asset="$1" tag="$2" dest="$3"
  local base="https://github.com/${REPO}/releases/download/${tag}"

  echo "Downloading ${asset}..."
  curl -fsSL -o "${dest}" "${base}/${asset}"

  echo "Verifying checksum..."
  local expected actual
  expected=$(curl -fsSL "${base}/checksums.txt" | awk -v f="${asset}" '$2 == f {print $1}')
  actual=$(sha256 "${dest}")
  if [[ -z ${expected} || ${expected} != "${actual}" ]]; then
    echo "Checksum mismatch for ${asset}" >&2
    echo "  Expected: ${expected}" >&2
    echo "  Actual:   ${actual}" >&2
    rm -f "${dest}"
    exit 1
  fi
  echo "  OK"
}

main "$@"
