#!/usr/bin/env bash
# Prints the number of web lint violations per gate rule across the whole web/src,
# ignoring web/eslint-baseline.txt, so the old complexity debt is measured (JOOXF-505).
# Only complexity (the gate) and the rules of hooks are reported; component and
# function length and parameter count are review signals, not gates (JOOXF-516).
# Local report only: it exits 0 whatever it finds.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

RULES=(complexity react-hooks/rules-of-hooks)

count_script='
const fs = require("node:fs");
const [reportPath, ...rules] = process.argv.slice(1);
const results = JSON.parse(fs.readFileSync(reportPath, "utf8"));
const counts = Object.fromEntries(rules.map((rule) => [rule, 0]));
let filesWithViolations = 0;
for (const result of results) {
  const hits = result.messages.filter((message) => message.ruleId in counts);
  if (hits.length > 0) {
    filesWithViolations += 1;
  }
  for (const hit of hits) {
    counts[hit.ruleId] += 1;
  }
}
console.log("web/src violations without the baseline (JOOXF-505):");
for (const rule of rules) {
  console.log("  " + rule.padEnd(28) + " " + counts[rule]);
}
const total = Object.values(counts).reduce((sum, count) => sum + count, 0);
console.log("  " + "total".padEnd(28) + " " + total);
console.log("files with a violation: " + filesWithViolations + " of " + results.length + " scanned");
'

report="$(mktemp)"
trap 'rm -f "$report"' EXIT

(
	cd "$ROOT/web"
	# eslint exits 1 when it finds violations; the counts below are the result, not the exit code.
	XFLICKR_WEB_LINT_NO_BASELINE=1 "$SCRIPT_DIR/run.sh" node npx eslint src --format json --output-file "$report" || true
)

"$SCRIPT_DIR/run.sh" node node -e "$count_script" "$report" "${RULES[@]}"
