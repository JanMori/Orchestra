#!/usr/bin/env bash
# Orchestra installer — installs the CLI and optionally provisions a self-host server.
#
# Install / upgrade CLI:
#   bash scripts/install.sh
#
# Install CLI + provision self-host server:
#   bash scripts/install.sh --with-server
#
# After installation, run `orchestra setup` to configure your environment.
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
REPO_URL="https://github.com/JanMori/Orchestra.git"
REPO_WEB_URL="https://github.com/JanMori/Orchestra"  # without .git, for GitHub web APIs
INSTALL_DIR="${ORCHESTRA_INSTALL_DIR:-$HOME/.orchestra/server}"

# Host ports Compose reported after `up -d`; set by setup_server and reused by
# the summary so the health check and the printed URLs cannot diverge.
SELFHOST_BACKEND_PORT=""
SELFHOST_FRONTEND_PORT=""

# Colors (disabled when not a terminal)
if [ -t 1 ] || [ -t 2 ]; then
  BOLD='\033[1m'
  GREEN='\033[0;32m'
  YELLOW='\033[0;33m'
  RED='\033[0;31m'
  CYAN='\033[0;36m'
  RESET='\033[0m'
else
  BOLD='' GREEN='' YELLOW='' RED='' CYAN='' RESET=''
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { printf "${BOLD}${CYAN}==> %s${RESET}\n" "$*"; }
ok()    { printf "${BOLD}${GREEN}✓ %s${RESET}\n" "$*"; }
warn()  { printf "${BOLD}${YELLOW}⚠ %s${RESET}\n" "$*" >&2; }
fail()  { printf "${BOLD}${RED}✗ %s${RESET}\n" "$*" >&2; exit 1; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

running_in_ssh_session() {
  [ -n "${SSH_CONNECTION:-}" ] || [ -n "${SSH_CLIENT:-}" ] || [ -n "${SSH_TTY:-}" ]
}

print_remote_server_token_hint() {
  if ! running_in_ssh_session; then
    return
  fi

  printf "  ${BOLD}Looks like a remote/SSH session.${RESET} Browser login may not be able to call back to this machine's localhost.\n"
  printf "  Token login is usually simpler here:\n"
  printf "     1. On your local computer, open Settings > API Tokens.\n"
  printf "     2. On this server, run:\n"
  printf "        ${CYAN}orchestra login --token <YOUR_TOKEN>${RESET}\n"
  printf "        ${CYAN}orchestra daemon start${RESET}\n"
  printf "\n"
}

# Host port Docker Compose actually published for a service.
compose_published_port() {
  local service=$1 container_port=$2 published

  published="$(docker compose -f docker-compose.selfhost.build.yml port "$service" "$container_port" 2>/dev/null | tail -n 1)"
  published="${published##*:}"
  published="${published%$'\r'}"

  case "$published" in
    "" | *[!0-9]*) return 1 ;;
  esac

  printf "%s" "$published"
}

detect_os() {
  case "$(uname -s)" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux" ;;
    MINGW*|MSYS*|CYGWIN*)
            fail "This script does not support Windows. Use PowerShell installer instead:
  powershell -ExecutionPolicy Bypass -File scripts/install.ps1" ;;
    *)      fail "Unsupported operating system: $(uname -s). Orchestra supports macOS, Linux, and Windows." ;;
  esac

  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       fail "Unsupported architecture: $ARCH" ;;
  esac
}

# ---------------------------------------------------------------------------
# CLI Installation
# ---------------------------------------------------------------------------
install_cli_binary() {
  info "Installing Orchestra CLI..."

  local tmp_dir
  tmp_dir=$(mktemp -d)

  # Check if we are in the source tree with Go installed
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

  if [ -d "$script_dir/server/cmd/orchestra" ] && command_exists go; then
    info "Building Orchestra CLI from local Go source..."
    (cd "$script_dir/server" && CGO_ENABLED=0 go build -o "$tmp_dir/orchestra" ./cmd/orchestra)
  elif [ -f "$script_dir/server/bin/orchestra" ]; then
    info "Using existing local Orchestra CLI binary..."
    cp "$script_dir/server/bin/orchestra" "$tmp_dir/orchestra"
  else
    local latest
    latest=$(curl -sI "$REPO_WEB_URL/releases/latest" 2>/dev/null | grep -i '^location:' | sed 's/.*tag\///' | tr -d '\r\n' || true)
    if [ -n "$latest" ]; then
      local version="${latest#v}"
      local url="${REPO_WEB_URL}/releases/download/${latest}/orchestra-cli-${version}-${OS}-${ARCH}.tar.gz"
      info "Downloading $url ..."
      if curl -fsSL "$url" -o "$tmp_dir/orchestra.tar.gz" 2>/dev/null; then
        tar -xzf "$tmp_dir/orchestra.tar.gz" -C "$tmp_dir" orchestra 2>/dev/null || true
      fi
    fi
  fi

  if [ ! -f "$tmp_dir/orchestra" ]; then
    if command_exists go && [ -d "$script_dir/server" ]; then
      (cd "$script_dir/server" && CGO_ENABLED=0 go build -o "$tmp_dir/orchestra" ./cmd/orchestra)
    fi
  fi

  if [ ! -f "$tmp_dir/orchestra" ]; then
    rm -rf "$tmp_dir"
    fail "Could not install Orchestra CLI. Ensure Go is installed or run `make build` inside the repository."
  fi

  chmod +x "$tmp_dir/orchestra"

  local bin_dir="${ORCHESTRA_BIN_DIR:-}"
  if [ -z "$bin_dir" ]; then
    if [ -w "/usr/local/bin" ]; then
      bin_dir="/usr/local/bin"
    else
      bin_dir="$HOME/.local/bin"
    fi
  fi
  mkdir -p "$bin_dir"
  mv "$tmp_dir/orchestra" "$bin_dir/orchestra"
  chmod +x "$bin_dir/orchestra"
  if ! echo "$PATH" | tr ':' '\n' | grep -q "^$bin_dir$"; then
    export PATH="$bin_dir:$PATH"
    add_to_path "$bin_dir"
  fi

  rm -rf "$tmp_dir"
  ok "Orchestra CLI installed to $bin_dir/orchestra"
}

add_to_path() {
  local dir="$1"
  local line="export PATH=\"$dir:\$PATH\""
  for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
    if [ -f "$rc" ] && ! grep -qF "$dir" "$rc"; then
      printf '\n# Added by Orchestra installer\n%s\n' "$line" >> "$rc"
    fi
  done
}

install_cli() {
  install_cli_binary

  if ! command_exists orchestra; then
    fail "CLI installed but 'orchestra' not found on PATH. You may need to restart your shell."
  fi
  local ver
  ver=$(orchestra version 2>/dev/null | awk 'NR==1{print $2}' || echo "ready")
  ok "Orchestra CLI is ready ($ver)"
}

# ---------------------------------------------------------------------------
# Docker check
# ---------------------------------------------------------------------------
check_docker() {
  if ! command_exists docker; then
    printf "\n"
    fail "Docker is not installed. Orchestra self-hosting requires Docker and Docker Compose.

Install Docker:
  macOS:  https://docs.docker.com/desktop/install/mac-install/
  Linux:  https://docs.docker.com/engine/install/

After installing Docker, re-run: bash scripts/install.sh --with-server"
  fi

  if ! docker info >/dev/null 2>&1; then
    fail "Docker is installed but not running. Please start Docker and re-run this script."
  fi

  ok "Docker is available"
}

# ---------------------------------------------------------------------------
# Server setup (self-host / --with-server)
# ---------------------------------------------------------------------------
setup_server() {
  info "Setting up Orchestra server..."
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

  if [ -n "${ORCHESTRA_INSTALL_DIR:-}" ]; then
    INSTALL_DIR="$ORCHESTRA_INSTALL_DIR"
    cd "$INSTALL_DIR"
  elif [ -f "$script_dir/docker-compose.selfhost.build.yml" ]; then
    INSTALL_DIR="$script_dir"
    cd "$INSTALL_DIR"
  else
    if [ -d "$INSTALL_DIR/.git" ]; then
      info "Updating existing installation at $INSTALL_DIR..."
      cd "$INSTALL_DIR"
    else
      info "Cloning Orchestra repository..."
      if ! command_exists git; then
        fail "Git is not installed. Please install git and re-run."
      fi
      if [ -d "$INSTALL_DIR" ]; then
        warn "Removing incomplete installation at $INSTALL_DIR..."
        rm -rf "$INSTALL_DIR"
      fi
      mkdir -p "$(dirname "$INSTALL_DIR")"
      git clone --depth 1 "$REPO_URL" "$INSTALL_DIR"
      cd "$INSTALL_DIR"
    fi
  fi

  ok "Repository ready at $INSTALL_DIR"

  # Generate .env if needed
  if [ ! -f .env ]; then
    info "Creating .env with random secrets..."
    cp .env.example .env
    local jwt pgpass
    jwt=$(openssl rand -hex 32)
    pgpass=$(openssl rand -hex 24)
    if [ "$(uname -s)" = "Darwin" ]; then
      sed -i '' "s/^JWT_SECRET=.*/JWT_SECRET=$jwt/" .env
      sed -i '' "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$pgpass/" .env
      sed -i '' -E "s#^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)#\1$pgpass\2#" .env
    else
      sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$jwt/" .env
      sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$pgpass/" .env
      sed -i -E "s#^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)#\1$pgpass\2#" .env
    fi
    ok "Generated .env with random JWT_SECRET and POSTGRES_PASSWORD"
  else
    ok "Using existing .env"
  fi

  # Start Docker Compose
  info "Starting Orchestra services (this may take a few minutes on first run)..."
  docker compose -f docker-compose.selfhost.build.yml up -d --build

  if ! SELFHOST_BACKEND_PORT="$(compose_published_port backend 8080)"; then
    fail "Started the stack but could not read the backend host port from Docker Compose.
  Check it with: cd $INSTALL_DIR && docker compose -f docker-compose.selfhost.build.yml ps"
  fi
  if ! SELFHOST_FRONTEND_PORT="$(compose_published_port frontend 3000)"; then
    fail "Started the stack but could not read the frontend host port from Docker Compose.
  Check it with: cd $INSTALL_DIR && docker compose -f docker-compose.selfhost.build.yml ps"
  fi

  # Wait for health check
  info "Waiting for backend to be ready..."
  local ready=false
  for i in $(seq 1 45); do
    if curl -sf "http://localhost:${SELFHOST_BACKEND_PORT}/health" >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 2
  done

  if [ "$ready" = true ]; then
    ok "Orchestra server is running"
  else
    warn "Server is still starting. You can check logs with:"
    echo "  cd $INSTALL_DIR && docker compose -f docker-compose.selfhost.build.yml logs"
    echo ""
  fi
}

# ---------------------------------------------------------------------------
# Main: Default mode (install CLI only)
# ---------------------------------------------------------------------------
run_default() {
  printf "\n"
  printf "${BOLD}  Orchestra — Installer${RESET}\n"
  printf "\n"

  detect_os
  install_cli

  printf "\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "${BOLD}${GREEN}  ✓ Orchestra CLI is ready!${RESET}\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "\n"
  printf "  ${BOLD}Next: configure your environment${RESET}\n"
  printf "\n"
  printf "     ${CYAN}orchestra setup${RESET}                # Configure + authenticate + start daemon\n"
  printf "     ${CYAN}orchestra setup self-host${RESET}       # Connect to a self-hosted server\n"
  printf "\n"
  print_remote_server_token_hint
  printf "  ${BOLD}Self-hosting?${RESET} Install the server first:\n"
  printf "     bash scripts/install.sh --with-server\n"
  printf "\n"
}

# ---------------------------------------------------------------------------
# Main: With-server mode
# ---------------------------------------------------------------------------
run_with_server() {
  printf "\n"
  printf "${BOLD}  Orchestra — Self-Host Installer${RESET}\n"
  printf "  Provisioning server infrastructure + installing CLI\n"
  printf "\n"

  detect_os
  check_docker
  setup_server
  install_cli

  printf "\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "${BOLD}${GREEN}  ✓ Orchestra server is running and CLI is ready!${RESET}\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "\n"
  printf "  ${BOLD}Frontend:${RESET}  http://localhost:%s\n" "$SELFHOST_FRONTEND_PORT"
  printf "  ${BOLD}Backend:${RESET}   http://localhost:%s\n" "$SELFHOST_BACKEND_PORT"
  printf "  ${BOLD}Server at:${RESET} %s\n" "$INSTALL_DIR"
  printf "\n"
  printf "  ${BOLD}Next: configure your CLI to connect${RESET}\n"
  printf "\n"
  printf "     ${CYAN}orchestra setup self-host${RESET}   # Configure + authenticate + start daemon\n"
  printf "\n"
  printf "  ${BOLD}To stop all services:${RESET}\n"
  printf "     bash scripts/install.sh --stop\n"
  printf "\n"
}

# ---------------------------------------------------------------------------
# Stop: shut down a self-hosted installation
# ---------------------------------------------------------------------------
run_stop() {
  printf "\n"
  info "Stopping Orchestra services..."

  if [ -d "$INSTALL_DIR" ]; then
    cd "$INSTALL_DIR"
    if [ -f docker-compose.selfhost.build.yml ]; then
      docker compose -f docker-compose.selfhost.build.yml down
      ok "Docker services stopped"
    fi
  fi

  if command_exists orchestra; then
    orchestra daemon stop 2>/dev/null && ok "Daemon stopped" || true
  fi

  printf "\n"
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
main() {
  local mode="default"

  while [ $# -gt 0 ]; do
    case "$1" in
      --with-server) mode="with-server" ;;
      --local)       mode="with-server" ;;
      --stop)        mode="stop" ;;
      --help|-h)
        echo "Usage: bash scripts/install.sh [--with-server | --stop]"
        echo ""
        echo "  (default)       Install / upgrade the Orchestra CLI"
        echo "  --with-server   Install CLI + provision a self-host server (Docker)"
        echo "  --stop          Stop a self-hosted installation"
        echo ""
        echo "Environment variables:"
        echo "  ORCHESTRA_INSTALL_DIR   Self-host server install directory"
        echo "                        (default: \$HOME/.orchestra/server)"
        echo "  ORCHESTRA_BIN_DIR       Target directory for the CLI binary"
        echo "                        (default: /usr/local/bin, then \$HOME/.local/bin)"
        echo ""
        echo "After installation, run 'orchestra setup' to configure your environment."
        exit 0
        ;;
      *) warn "Unknown option: $1" ;;
    esac
    shift
  done

  case "$mode" in
    default)     run_default ;;
    with-server) run_with_server ;;
    stop)        run_stop ;;
  esac
}

main "$@"
