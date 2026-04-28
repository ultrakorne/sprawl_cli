#!/usr/bin/env bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

SKILL_SRC="$REPO_ROOT/.claude/skills/sprawl"
CLAUDE_AGENT_SRC="$REPO_ROOT/.claude/agents/sprawl-bookkeeper.md"
OPENCODE_AGENT_SRC="$REPO_ROOT/.opencode/agents/sprawl-bookkeeper.md"

CLAUDE_SKILL_DST="$HOME/.claude/skills/sprawl"
OPENCODE_SKILL_DST="$HOME/.config/opencode/skills/sprawl"
CLAUDE_AGENT_DST="$HOME/.claude/agents/sprawl-bookkeeper.md"
OPENCODE_AGENT_DST="$HOME/.config/opencode/agents/sprawl-bookkeeper.md"

if [ ! -d "$SKILL_SRC" ]; then
    echo -e "${RED}Error: skill source not found at $SKILL_SRC${NC}"
    exit 1
fi

if [ ! -f "$CLAUDE_AGENT_SRC" ]; then
    echo -e "${RED}Error: Claude Code agent source not found at $CLAUDE_AGENT_SRC${NC}"
    exit 1
fi

if [ ! -f "$OPENCODE_AGENT_SRC" ]; then
    echo -e "${RED}Error: OpenCode agent source not found at $OPENCODE_AGENT_SRC${NC}"
    exit 1
fi

echo -e "${YELLOW}Installing sprawl skill + agent${NC}"
echo ""

echo -n "Skill → Claude Code ($CLAUDE_SKILL_DST)... "
mkdir -p "$(dirname "$CLAUDE_SKILL_DST")"
rm -rf "$CLAUDE_SKILL_DST"
cp -r "$SKILL_SRC" "$CLAUDE_SKILL_DST"
echo -e "${GREEN}Done${NC}"

echo -n "Skill → OpenCode ($OPENCODE_SKILL_DST)... "
mkdir -p "$(dirname "$OPENCODE_SKILL_DST")"
rm -rf "$OPENCODE_SKILL_DST"
cp -r "$SKILL_SRC" "$OPENCODE_SKILL_DST"
echo -e "${GREEN}Done${NC}"

echo -n "Agent → Claude Code ($CLAUDE_AGENT_DST)... "
mkdir -p "$(dirname "$CLAUDE_AGENT_DST")"
cp "$CLAUDE_AGENT_SRC" "$CLAUDE_AGENT_DST"
echo -e "${GREEN}Done${NC}"

echo -n "Agent → OpenCode ($OPENCODE_AGENT_DST)... "
mkdir -p "$(dirname "$OPENCODE_AGENT_DST")"
cp "$OPENCODE_AGENT_SRC" "$OPENCODE_AGENT_DST"
echo -e "${GREEN}Done${NC}"

echo ""
echo -e "${GREEN}sprawl skill + agent installed.${NC}"
