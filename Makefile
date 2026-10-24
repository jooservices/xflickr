.DEFAULT_GOAL := help

.PHONY: help lint arch-lint test test-integration e2e gen gen-stale check ui-spec-check dev-up dev-down

help:
	@./docker/scripts/help.sh

lint:
	@./docker/scripts/lint.sh

arch-lint:
	@./docker/scripts/arch-lint.sh

test:
	@./docker/scripts/test.sh

test-integration:
	@./docker/scripts/test-integration.sh

e2e:
	@./docker/scripts/e2e.sh

gen:
	@./docker/scripts/gen.sh

gen-stale:
	@./docker/scripts/gen-stale.sh

check:
	@./docker/scripts/check.sh

# Node 24 loads a directory argument as a module. The glob runs the tests under scripts/.
ui-spec-check:
	@./docker/scripts/run.sh node node scripts/ui-spec-check.mjs
	@./docker/scripts/run.sh node node --test "scripts/**/*.test.mjs"

dev-up:
	@./docker/scripts/dev.sh up

dev-down:
	@./docker/scripts/dev.sh down
