# AI Workflow

This repository uses labels to hand work between a human, Cursor, Codex, and
CI.

## Required Secret

Add the repository secret `OPENAI_API_KEY`. The Codex workflows fail with a
clear error if the secret is missing.

## Issue Brief Flow

1. Add `needs-brief` to an issue.
2. Cursor writes a comment that includes `<!-- ai-brief -->`.
3. The `AI brief gate` workflow asks Codex to decide whether the brief is ready.
4. If Codex returns `APPROVE`, the workflow adds `ready-for-build` and removes
   `needs-brief`.
5. If Codex returns `CHANGE`, it leaves `needs-brief` in place and comments with
   requested changes.
6. If Codex returns `REJECT`, it removes `ready-for-build` and `needs-brief`.

Only comments from trusted repository actors are evaluated.

## Pull Request Flow

1. Cursor opens a PR from a branch in this repository.
2. CI runs.
3. If CI fails, the `AI PR review gate` workflow adds `needs-ai-fix` and removes
   `ready-for-human`.
4. If CI passes, Codex reviews the PR diff from GitHub API data.
5. If Codex returns `FAIL`, the workflow adds `needs-ai-fix` and removes
   `ready-for-human`.
6. If Codex returns `PASS`, the workflow adds `ready-for-human` and removes
   `needs-ai-fix`.

Only open PRs from trusted same-repository branches are reviewed by Codex.

