#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

run_python_test() {
	python3 "$SCRIPT_DIR/$1"
}

run_python_test test_ci_parity.py
run_python_test test_ci_changes.py
run_python_test test_tool_versions.py
run_python_test test_clean_db.py
run_python_test test_ci_aggregate.py
run_python_test test_integration_shards.py
run_python_test test_merge_coverage.py
run_python_test test_integration_test_count.py
run_python_test migration_versions_test.py
run_python_test test_check_go_version.py
run_python_test test_coverage_gate.py
run_python_test test_obs_alert_config.py
run_python_test test_worktree_env.py
run_python_test module_cycles_test.py
run_python_test test_sqlc_stale.py
"$SCRIPT_DIR/api-usage-check.test.sh"
"$SCRIPT_DIR/ui-e2e-coverage.test.sh"
"$SCRIPT_DIR/db-report.test.sh"
"$SCRIPT_DIR/compare-db.test.sh"
"$SCRIPT_DIR/ui-spec-check.test.sh"
"$SCRIPT_DIR/ui-spec-check.sh"
"$SCRIPT_DIR/run.sh" shellcheck shellcheck "$SCRIPT_DIR"/*.sh
python3 "$SCRIPT_DIR/ci_parity.py"
python3 "$SCRIPT_DIR/check_tool_versions.py"
"$SCRIPT_DIR/commitlint.sh"

if [[ "${CI_CHANGED_GO:-true}" == "true" ]]; then
	python3 "$SCRIPT_DIR/check_go_version.py"
	make -C "$ROOT" --no-print-directory precheck
	python3 "$SCRIPT_DIR/migration_versions.py" check
	python3 "$SCRIPT_DIR/go_function_gate_test.py"
	python3 "$SCRIPT_DIR/go_function_gate.py"
	"$SCRIPT_DIR/arch-lint.sh"
fi

printf '\nStatic checks passed.\n'
