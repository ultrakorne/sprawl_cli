#!/usr/bin/env bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Target directories
CLAUDE_SKILLS_DIR="$HOME/.claude/skills"
OPENCODE_SKILLS_DIR="$HOME/.config/opencode/skills"

# Script directory and repo root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SKILLS_SRC_DIR="$REPO_ROOT/.claude/skills"

usage() {
    echo "Usage: $0 <skill-name-or-path>"
    echo ""
    echo "Installs or updates a skill to Claude Code and OpenCode."
    echo ""
    echo "Arguments:"
    echo "  skill-name-or-path    Skill folder name (e.g. 'sprawl') or path to"
    echo "                        its directory (e.g. '.claude/skills/sprawl/')"
    echo ""
    echo "Available skills:"
    if [ -d "$SKILLS_SRC_DIR" ]; then
        for dir in "$SKILLS_SRC_DIR"/*/; do
            [ -d "$dir" ] || continue
            echo "  - $(basename "$dir")"
        done
    fi
    exit 1
}

# Check if skill name is provided
if [ -z "$1" ]; then
    echo -e "${RED}Error: No skill name provided${NC}"
    echo ""
    usage
fi

ARG="${1%/}" # strip trailing slash

# Resolve SKILL_SOURCE: accept a path (relative or absolute) or a bare name.
if [ -d "$ARG" ]; then
    SKILL_SOURCE="$(cd "$ARG" && pwd)"
elif [ -d "$SKILLS_SRC_DIR/$ARG" ]; then
    SKILL_SOURCE="$SKILLS_SRC_DIR/$ARG"
else
    echo -e "${RED}Error: Skill '$ARG' not found${NC}"
    echo -e "${RED}  Looked for: $ARG${NC}"
    echo -e "${RED}  Looked for: $SKILLS_SRC_DIR/$ARG${NC}"
    echo ""
    usage
fi

SKILL_NAME="$(basename "$SKILL_SOURCE")"

echo -e "${YELLOW}Installing skill: $SKILL_NAME${NC}"
echo -e "  from $SKILL_SOURCE"
echo ""

# Install to Claude Code
echo -n "Installing to Claude Code ($CLAUDE_SKILLS_DIR)... "
mkdir -p "$CLAUDE_SKILLS_DIR"
rm -rf "$CLAUDE_SKILLS_DIR/$SKILL_NAME"
cp -r "$SKILL_SOURCE" "$CLAUDE_SKILLS_DIR/$SKILL_NAME"
echo -e "${GREEN}Done${NC}"

# Install to OpenCode
echo -n "Installing to OpenCode ($OPENCODE_SKILLS_DIR)... "
mkdir -p "$OPENCODE_SKILLS_DIR"
rm -rf "$OPENCODE_SKILLS_DIR/$SKILL_NAME"
cp -r "$SKILL_SOURCE" "$OPENCODE_SKILLS_DIR/$SKILL_NAME"
echo -e "${GREEN}Done${NC}"

echo ""
echo -e "${GREEN}Skill '$SKILL_NAME' installed successfully!${NC}"
