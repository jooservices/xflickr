.DEFAULT_GOAL := help

.PHONY: help static verify lint arch-lint web-lint web-test go-test go-coverage clean-db test test-integration integration-count e2e gen gen-stale check check-go-version check-tool-versions ci-changes ci-aggregate security security-backend security-web precheck next-migration ui-spec-check dev-up dev-down develop-up develop-down develop-logs develop-status

help:
	@./docker/scripts/help.sh

static:
	@./docker/scripts/static.sh

verify:
	@python3 docker/scripts/verify.py

lint:
	@./docker/scripts/lint.sh

arch-lint:
	@./docker/scripts/arch-lint.sh

web-lint:
	@./docker/scripts/web-lint.sh

web-test:
	@./docker/scripts/web-test.sh

go-test:
	@./docker/scripts/go-test.sh

go-coverage:
	@./docker/scripts/go-coverage.sh

clean-db:
	@python3 docker/scripts/clean_db.py

test:
	@./docker/scripts/test.sh

test-integration:
	@./docker/scripts/test-integration.sh

integration-count:
	@./docker/scripts/integration-count.sh

check-tool-versions:
	@python3 docker/scripts/check_tool_versions.py

ci-changes:
	@python3 docker/scripts/ci_changes.py

ci-aggregate:
	@python3 docker/scripts/ci_aggregate.py

security:
	@$(MAKE) --no-print-directory security-backend
	@$(MAKE) --no-print-directory security-web

security-backend:
	@bash docker/scripts/security-audit-backend.test.sh
	@bash docker/scripts/security-audit-backend.sh

security-web:
	@bash docker/scripts/security-audit-web.test.sh
	@bash docker/scripts/security-audit-web.sh

e2e:
	@./docker/scripts/ui-e2e-coverage.sh
	@./docker/scripts/e2e.sh

gen:
	@./docker/scripts/gen.sh

gen-stale:
	@./docker/scripts/gen-stale.sh

check:
	@./docker/scripts/check.sh

check-go-version:
	@python3 docker/scripts/check_go_version.py

precheck:
	@python3 docker/scripts/migration_versions.py check

next-migration:
	@python3 docker/scripts/migration_versions.py next "$(NAME)"

# Node 24 loads a directory argument as a module. The glob runs the tests under scripts/.
ui-spec-check:
	@./docker/scripts/run.sh node node scripts/ui-spec-check.mjs
	@./docker/scripts/run.sh node node --test "scripts/**/*.test.mjs"
	@./docker/scripts/run.sh node npx --yes markdownlint-cli@0.44.0 --config docs/ui/.markdownlint.yaml docs/ui/components.md

dev-up:
	@./docker/scripts/dev.sh up

dev-down:
	@./docker/scripts/dev.sh down

develop-up:
	@./docker/scripts/develop-env.sh up

develop-down:
	@./docker/scripts/develop-env.sh down

develop-logs:
	@./docker/scripts/develop-env.sh logs

develop-status:
	@./docker/scripts/develop-env.sh status
