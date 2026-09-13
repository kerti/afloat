#!/bin/sh
# SessionStart hook - orient the agent, and fast-forward main only when it is
# safe: already on main, with a clean tree. Any other branch is left alone.
#
# Emits JSON on stdout: additionalContext goes into the agent's context,
# systemMessage is shown to the human. Never fails the session - worst case it
# prints nothing.
set -eu

cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0
command -v jq >/dev/null 2>&1 || exit 0

emit() { # emit <additionalContext> [systemMessage]
  if [ $# -ge 2 ] && [ -n "$2" ]; then
    jq -n --arg ac "$1" --arg sm "$2" \
      '{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:$ac},systemMessage:$sm}'
  else
    jq -n --arg ac "$1" \
      '{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:$ac}}'
  fi
  exit 0
}

branch=$(git branch --show-current 2>/dev/null || echo "")
[ -n "$branch" ] || exit 0
dirty=$(git status --porcelain 2>/dev/null | grep -c . || true)

if [ ! -f Makefile ]; then
  emit "On branch '$branch'. No Makefile yet - see BOOTSTRAP.md §11 for scaffold order."
fi

if [ "$dirty" -gt 0 ]; then
  emit "On branch '$branch', working tree dirty ($dirty change(s))."
fi

if [ "$branch" != "main" ]; then
  emit "On feature branch '$branch', clean tree @ $(git rev-parse --short HEAD). Left alone - main is only fast-forwarded from a clean 'main' branch."
fi

if git remote get-url origin >/dev/null 2>&1 && git fetch origin main --quiet 2>/dev/null; then
  if git merge-base --is-ancestor HEAD origin/main 2>/dev/null; then
    if git merge --ff-only origin/main --quiet 2>/dev/null; then
      emit "On 'main', fast-forwarded to $(git rev-parse --short HEAD)."
    fi
  fi
fi

emit "On 'main', up to date @ $(git rev-parse --short HEAD)."
