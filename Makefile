.DEFAULT_GOAL := help

.PHONY: help lint arch-lint test test-integration e2e gen check dev-up dev-down

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

check:
	@./docker/scripts/check.sh

dev-up:
	@./docker/scripts/dev.sh up

dev-down:
	@./docker/scripts/dev.sh down
