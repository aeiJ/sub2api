.PHONY: build build-backend build-frontend build-datamanagementd test test-backend test-frontend test-frontend-critical test-datamanagementd secret-scan prod-state prod-backup-branch release-check release-image rollback-help

RELEASE ?=
IMAGE ?= sub2api
PROD_BRANCH ?= prod

FRONTEND_CRITICAL_VITEST := \
	src/views/auth/__tests__/LinuxDoCallbackView.spec.ts \
	src/views/auth/__tests__/WechatCallbackView.spec.ts \
	src/views/user/__tests__/PaymentView.spec.ts \
	src/views/user/__tests__/PaymentResultView.spec.ts \
	src/components/user/profile/__tests__/ProfileInfoCard.spec.ts \
	src/views/admin/__tests__/SettingsView.spec.ts

# 一键编译前后端
build: build-backend build-frontend

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译前端（需要已安装依赖）
build-frontend:
	@pnpm --dir frontend run build

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

test-frontend:
	@pnpm --dir frontend run lint:check
	@pnpm --dir frontend run typecheck
	@$(MAKE) test-frontend-critical

test-frontend-critical:
	@pnpm --dir frontend exec vitest run $(FRONTEND_CRITICAL_VITEST)

test-datamanagementd:
	@cd datamanagement && go test ./...

secret-scan:
	@python3 tools/secret_scan.py

prod-state:
	@git fetch origin --prune --tags
	@echo "current_branch=$$(git branch --show-current)"
	@echo "current_sha=$$(git rev-parse --short=12 HEAD)"
	@if git show-ref --verify --quiet refs/heads/$(PROD_BRANCH); then \
		echo "$(PROD_BRANCH)_sha=$$(git rev-parse --short=12 $(PROD_BRANCH))"; \
	else \
		echo "$(PROD_BRANCH)_sha=missing-local-branch"; \
	fi
	@if git show-ref --verify --quiet refs/remotes/origin/$(PROD_BRANCH); then \
		echo "origin/$(PROD_BRANCH)_sha=$$(git rev-parse --short=12 origin/$(PROD_BRANCH))"; \
	else \
		echo "origin/$(PROD_BRANCH)_sha=missing-remote-branch"; \
	fi

prod-backup-branch:
	@if ! git show-ref --verify --quiet refs/heads/$(PROD_BRANCH); then \
		echo "Missing local $(PROD_BRANCH) branch. Run: git fetch origin $(PROD_BRANCH):$(PROD_BRANCH)"; \
		exit 2; \
	fi
	@old_sha="$$(git rev-parse --short=8 $(PROD_BRANCH))"; \
	backup_branch="backup/pre-prod-$$(date +%Y%m%d)-$${old_sha}"; \
	git branch "$${backup_branch}" $(PROD_BRANCH); \
	echo "created $${backup_branch} -> $${old_sha}"

release-check: test-backend test-frontend build-backend build-frontend

release-image:
	@if [ -z "$(RELEASE)" ]; then \
		echo "Usage: make release-image RELEASE=vX.Y.Z-prod.N IMAGE=registry/repo"; \
		exit 2; \
	fi
	@deploy/build_prod_image.sh "$(RELEASE)" "$(IMAGE)"

rollback-help:
	@echo "Rollback policy: prefer the previous verified immutable Docker image tag; do not rewrite $(PROD_BRANCH)."
	@echo "1. Switch deployment to the previous known-good image tag or digest."
	@echo "2. Verify Docker health and public /health."
	@echo "3. Create a Beads incident issue with bad tag, restored tag, impact, and follow-up owner."
	@echo "4. If code must change, branch hotfix/<bd-id>-<slug> from $(PROD_BRANCH), validate, and release vX.Y.Z-prod.N+1."
