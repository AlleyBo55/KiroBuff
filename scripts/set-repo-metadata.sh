#!/usr/bin/env sh
# Sets the GitHub description, homepage and topics.
#
# These cannot be set from a commit, and the repository has none of them. GitHub
# weights the description heavily in search, Google uses it as the result
# snippet, and topic pages are how people browse.
#
# Requires the gh CLI, authenticated:  gh auth login
set -eu

REPO="${REPO:-AlleyBo55/KiroBuff}"

command -v gh >/dev/null 2>&1 || {
  echo "gh is required: https://cli.github.com" >&2
  echo "Or paste the values from docs/discoverability.md into repository settings." >&2
  exit 1
}

gh repo edit "$REPO" \
  --description "Stop your AI coding agent breaking things. Enforced guardrails that block unsafe edits, composable modes, and token-cost budgeting for Kiro CLI." \
  --homepage "https://pkg.go.dev/github.com/AlleyBo55/KiroBuff" \
  --add-topic ai-coding-assistant --add-topic ai-agents --add-topic agentic-ai \
  --add-topic ai-safety --add-topic guardrails --add-topic llm-tools \
  --add-topic developer-tools --add-topic devtools --add-topic cli \
  --add-topic golang --add-topic git-hooks --add-topic code-quality \
  --add-topic conventional-commits --add-topic llm-cost-optimization \
  --add-topic prompt-engineering --add-topic claude --add-topic anthropic \
  --add-topic kiro --add-topic vibe-coding --add-topic ai-code-review

echo "done. verify:  gh repo view $REPO"
