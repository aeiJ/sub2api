# Codex Instructions

## Beads

Use Beads (`bd`) for durable task tracking. Create or claim a Beads issue before
making code, branch, release, or deployment changes.

## Git Branch Management

Strictly follow [docs/git-branch-management.md](docs/git-branch-management.md)
before creating, renaming, switching, merging, or deleting branches.

- Do not create ad hoc branch names.
- Feature branches must be named `feature/<bd-id>-<slug>`.
- Hotfix branches must be named `hotfix/<bd-id>-<slug>`.
- Release branches must be named `release/vX.Y.Z-prod.N`.
- Upstream sync branches must be named `codex/sync-vX.Y.Z`.
- Backup and rollback branches must use the documented `backup/...` and
  `rollback/...` formats.
- For `feature/*` and `hotfix/*`, `<bd-id>` must be the exact Beads issue id
  and `<slug>` must use only lowercase letters, digits, and hyphens.
- Do not use dots, underscores, spaces, or extra slash segments in feature or
  hotfix slugs. Convert versions like `v0.1.146` to `v0-1-146`.
- Before running `git switch -c`, `git branch`, `git worktree add -b`, or
  `git branch -m`, validate the target branch name against these rules.

When asked to merge the latest production branch, use `prod` unless the user
explicitly names a different branch to merge.
