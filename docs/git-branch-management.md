# Git Branch Management and Rollback

This repository uses a fixed production branch and image-first rollback.
`prod` is the only Git branch that represents production. Versioned
`codex/sync-v*` branches are upstream synchronization branches only; they are
not long-lived production branches.

中文摘要：生产入口固定为 `prod`。上线先经过 `release/*` 候选分支和镜像验证；生产故障优先切回上一个已验证 Docker 不可变镜像 tag，不通过
`git reset --hard` 或 force push 改写 `prod`。

## Project Entry Points

Use these project commands instead of relying on chat history:

```bash
# Show current branch, HEAD, local prod, and origin/prod.
make prod-state

# Create backup/pre-prod-YYYYMMDD-<sha> before moving prod.
make prod-backup-branch

# Run release quality gates.
make release-check

# Build linux/amd64 production candidate image.
make release-image RELEASE=v0.1.143-prod.1 IMAGE=dockeraeij/sub2api

# Print rollback policy and required steps.
make rollback-help
```

All release, hotfix, rollback, and production incident work must have a Beads
issue. Use `bd show <id>` as the durable source of task context.

## Branch Naming Enforcement

Branch names are part of the project contract. Validate branch names before
creating or renaming branches; do not create temporary exceptions.

Allowed branch forms:

- `feature/<bd-id>-<slug>`
- `hotfix/<bd-id>-<slug>`
- `release/vX.Y.Z-prod.N`
- `codex/sync-vX.Y.Z`
- `prod`
- `backup/pre-prod-YYYYMMDD-<sha>`
- `rollback/prod-YYYYMMDD-<sha>`

For `feature/*` and `hotfix/*` branches:

- `<bd-id>` must be the exact Beads issue id, for example `sub2api-b0m`.
- `<slug>` must use only lowercase letters, digits, and hyphens.
- Do not use dots, underscores, spaces, or extra slash segments in the slug.
- Convert version strings in slugs from dots to hyphens, for example
  `v0.1.146` becomes `v0-1-146`.

Examples:

```bash
# Good
git switch -c feature/sub2api-b0m-enforce-branch-naming prod
git worktree add -b feature/sub2api-d4z-merge-sync-v0-1-146 ../merge-check codex/upstream-apikey-channel-monitor

# Bad
git switch -c feature/sub2api-b0m
git switch -c feature/sub2api-d4z-merge-sync-v0.1.146
git switch -c codex/my-feature-branch
```

## Branch Roles

| Branch | Purpose | Rules |
| --- | --- | --- |
| `prod` | Current production code | Move only after release validation. Do not force-push. |
| `release/vX.Y.Z-prod.N` | Production candidate | Cut from an upstream tag or a verified sync branch. |
| `feature/<bd-id>-<slug>` | Feature work | Link every branch to a Beads issue. |
| `hotfix/<bd-id>-<slug>` | Urgent production fix | Cut from `prod`; after validation merge back to `prod` and the next release. |
| `codex/sync-vX.Y.Z` | Upstream sync/conflict resolution | Temporary source for release branches, not production. |
| `backup/pre-prod-YYYYMMDD-<sha>` | Previous production pointer | Create before moving `prod`. Keep it read-only. |
| `rollback/prod-YYYYMMDD-<sha>` | Optional explicit rollback pointer | Use when a named rollback branch is clearer than a generic backup branch. |

Do not use `main` as the production rollback mechanism. `main` may exist for
collaboration or upstream alignment, but production rollback should be based on
previous verified image tags plus a backup/rollback branch pointer.

## Starting Work

Create or claim the Beads issue first:

```bash
bd create --title="Short summary" --description="Why this exists and what needs to change" --type=task --priority=2
bd update <bd-id> --claim
```

Create a branch that names the issue:

```bash
git fetch origin upstream --tags
git switch -c feature/<bd-id>-<slug> prod
```

Use `hotfix/<bd-id>-<slug>` instead of `feature/*` for urgent production fixes:

```bash
git fetch origin --tags
git switch -c hotfix/<bd-id>-<slug> prod
```

## Creating a Release Candidate

Start from an upstream tag or a verified sync branch:

```bash
git fetch origin upstream --tags
git switch -c release/v0.1.143-prod.1 codex/sync-v0.1.143
```

Merge only approved feature or hotfix branches into the release candidate:

```bash
git merge --no-ff feature/<bd-id>-<slug>
git merge --no-ff hotfix/<bd-id>-<slug>
```

Run the release gates:

```bash
make release-check
make release-image RELEASE=v0.1.143-prod.1 IMAGE=dockeraeij/sub2api
```

`release-check` runs backend tests, frontend lint/typecheck/critical tests, and
backend/frontend builds. `release-image` calls `deploy/build_prod_image.sh`,
which builds `linux/amd64` and creates:

- `IMAGE:vX.Y.Z-prod.N-<shortsha>`
- `IMAGE:prod-candidate`

If the release touches only one area, a narrower test set is acceptable during
feature development, but the final release candidate should use the full gate.

## Promoting to Production

Before moving `prod`, save the current production pointer:

```bash
git fetch origin --tags
git switch prod
git pull --ff-only origin prod
make prod-backup-branch
```

After the release candidate and immutable image are validated, move `prod` with
a normal fast-forward or reviewed merge:

```bash
git merge --ff-only release/v0.1.143-prod.1
git push origin prod
git push origin backup/pre-prod-YYYYMMDD-<sha>
```

Deploy the immutable image tag that was validated. Move mutable tags such as
`latest`, `prod`, or `prod-candidate` only after the immutable tag has passed
local smoke tests.

Record the deployment in Beads:

```bash
bd update <bd-id> --notes "Deployed IMAGE:vX.Y.Z-prod.N-<shortsha>; prod=<sha>; rollback=<previous-image-tag>"
```

## Production Verification

Verify both the container and public endpoint:

```bash
docker inspect <container> --format '{{if .State.Health}}{{.State.Health.Status}}{{end}} {{.Image}}'
docker exec <container> /app/sub2api --version
curl -fsS https://<domain>/health
```

For frontend changes, also verify that the public HTML references the new asset
names and that those JS/CSS assets return `200`. `/health` can be healthy while
an old frontend container or cached asset set is still being served.

## Rollback

Rollback should use image tags first, not Git history rewrites.

1. Switch the deployment back to the previous known-good immutable image tag or
   digest.
2. Verify Docker health, `/health`, core login/API flows, and recent logs.
3. Create or update a Beads incident issue with the bad image tag, restored tag,
   commit SHAs, impact, and follow-up owner.
4. If code must change, cut `hotfix/<bd-id>-<slug>` from `prod`, validate it,
   and publish `vX.Y.Z-prod.N+1`.

Do not use `git reset --hard` or force-push `prod` for rollback unless the branch
is private and nobody else depends on it.

Use the project reminder when operating manually:

```bash
make rollback-help
```

## Current Release Example

The first production release managed with this workflow used:

- `prod` at `f372af822e6e`
- `release/v0.1.143-prod.1`
- rollback branch `rollback/prod-20260706-e6634c06`
- Docker image `dockeraeij/sub2api:v0.1.143-prod.1-f372af82`
- digest `sha256:8ee8ae50d6b338e652f6691b85d7f001909227a0e7510d8d34aec6ac3293e8a7`

Treat these as examples of the format, not as the only valid release values.
