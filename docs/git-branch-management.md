# Git Branch Management and Rollback

This repository uses a lightweight production workflow. The fixed `prod` branch is
the only Git branch that represents production. Versioned `codex/sync-v*`
branches are synchronization branches only.

## Branch Roles

| Branch | Purpose | Rules |
| --- | --- | --- |
| `prod` | Current production code | Move only after release validation. Do not force-push. |
| `release/vX.Y.Z-prod.N` | Production candidate | Cut from an upstream tag or a verified sync branch. |
| `feature/<bd-id>-<slug>` | Feature work | Link every branch to a Beads issue. |
| `hotfix/<bd-id>-<slug>` | Urgent production fix | Cut from `prod`, merge back into `prod` and the next release branch. |
| `codex/sync-vX.Y.Z` | Upstream sync/conflict resolution | Temporary source for release branches, not production. |
| `backup/pre-prod-YYYYMMDD-<sha>` | Previous production pointer | Create before moving `prod`. |

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

Use `hotfix/<bd-id>-<slug>` instead of `feature/*` for urgent production fixes.

## Creating a Release Candidate

Start from an upstream tag or a verified sync branch:

```bash
git fetch origin upstream --tags
git switch -c release/v0.1.143-prod.1 codex/sync-v0.1.143
```

Merge only the approved feature or hotfix branches into the release candidate.
Run the quality gates that match the change:

```bash
# Backend changes
cd backend && make test-unit
cd backend && make test-integration

# Frontend changes
make test-frontend

# Cross-module or release validation
deploy/build_prod_image.sh v0.1.143-prod.1 sub2api
```

The production image helper builds `linux/amd64` and creates both:

- `sub2api:v0.1.143-prod.1-<shortsha>`
- `sub2api:prod-candidate`

## Promoting to Production

Before moving `prod`, save the old production pointer:

```bash
old_sha="$(git rev-parse --short=8 prod)"
git branch "backup/pre-prod-$(date +%Y%m%d)-${old_sha}" prod
```

After the release candidate is validated, move `prod` with a normal merge or
fast-forward:

```bash
git switch prod
git merge --ff-only release/v0.1.143-prod.1
```

Deploy the immutable image tag that was validated. Move the mutable `prod` image
tag only after the immutable tag has passed production checks.

## Rollback

Rollback should use image tags first, not Git history rewrites.

1. Point the deployment back to the previous known-good immutable image tag.
2. Create a Beads incident issue with the bad tag, restored tag, impact, and
   follow-up owner.
3. If code must change, cut `hotfix/<bd-id>-<slug>` from `prod`, validate it,
   and publish a new release candidate.

Do not use `git reset --hard` or force-push `prod` for rollback unless the branch
is private and nobody else depends on it.

