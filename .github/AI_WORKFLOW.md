# AI Workflow

This repository uses GitHub labels and Codex GitHub mentions to hand work
between a human, Cursor, Codex, and CI. It does not require an OpenAI API key in
GitHub Actions.

## Setup

1. Set up Codex cloud for this repository.
2. Enable Codex code review for this repository in Codex settings.
3. Keep these labels available:
   - `needs-brief`
   - `ready-for-build`
   - `needs-ai-fix`
   - `ready-for-human`

## Issue Brief Flow

1. Add `needs-brief` to an issue.
2. Cursor writes a brief comment that includes `<!-- ai-brief -->`.
3. `AI issue brief router` posts a bounded `@codex` request.
4. Codex replies with one of:
   - `codex-brief: APPROVE`
   - `codex-brief: CHANGE`
   - `codex-brief: REJECT`
5. `APPROVE` adds `ready-for-build` and removes `needs-brief`.
6. `CHANGE` keeps `needs-brief` and removes `ready-for-build`.
7. `REJECT` removes both `needs-brief` and `ready-for-build`.

Trusted maintainers can use the same `codex-brief:` line manually if the Codex
GitHub integration does not respond.

## Pull Request Flow

1. Cursor opens a PR from a branch in this repository.
2. CI runs.
3. If CI fails, `AI PR gate` adds `needs-ai-fix` and removes
   `ready-for-human`.
4. If CI passes, `AI PR gate` removes stale handoff labels and posts
   `@codex review` once for that commit.
5. When Codex posts a review:
   - review comments or requested changes add `needs-ai-fix`;
   - a clean review adds `ready-for-human`.

Only open same-repository PRs from trusted repository actors are routed.
