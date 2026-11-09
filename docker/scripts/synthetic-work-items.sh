#!/usr/bin/env bash
# Generate a synthetic work_items load for JOOXF-370 EXPLAIN / HOT tests.
# Usage: synthetic-work-items.sh <database_url> [pending_rows=350000] [total_rows=500000]
set -euo pipefail

db_url="${1:?database url required}"
pending="${2:-350000}"
total="${3:-500000}"
if [[ ! "$pending" =~ ^[0-9]+$ || ! "$total" =~ ^[0-9]+$ ]]; then
  echo "row counts must be non-negative integers" >&2
  exit 1
fi
pending=$((10#$pending))
total=$((10#$total))
if (( pending > total )); then
  echo "pending_rows cannot exceed total_rows" >&2
  exit 1
fi
psql "$db_url" -v ON_ERROR_STOP=1 <<SQL
DO \$\$
DECLARE
  uid BIGINT;
  kid BIGINT;
BEGIN
  INSERT INTO users(email, password_hash, role)
  VALUES ('synthetic-work-items@example.test', 'unused', 'user')
  ON CONFLICT (email) DO NOTHING;
  SELECT id INTO uid FROM users WHERE email = 'synthetic-work-items@example.test';
  INSERT INTO api_keys(user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst, status)
  VALUES (uid, 'synthetic', 'synthetic-public', '{}'::jsonb, 3600, 4, 5, 'valid')
  ON CONFLICT (user_id, key_public) DO UPDATE SET status = 'valid'
  RETURNING id INTO kid;
  IF kid IS NULL THEN
    SELECT id INTO kid FROM api_keys WHERE user_id = uid AND key_public = 'synthetic-public';
  END IF;
  INSERT INTO flickr_accounts(user_id, key_id, nsid, username, token_sealed, secret_sealed, status)
  VALUES (uid, kid, 'synthetic@N00', 'synthetic', '{}'::jsonb, '{}'::jsonb, 'ok')
  ON CONFLICT (user_id, nsid) DO UPDATE SET status = 'ok', key_id = EXCLUDED.key_id;
  DELETE FROM work_items WHERE subject_key LIKE 'synthetic-wi-%';
  INSERT INTO work_items(kind, subject_key, user_id, key_id, state, worker_type, score, depth, created_at, not_before)
  SELECT 'sync_person',
         'synthetic-wi-' || g,
         uid,
         kid,
         CASE WHEN g <= ${pending} THEN 'pending' ELSE 'done' END,
         'sync',
         (random() * 1000)::double precision,
         (g % 3),
         now() - ((g % 10000) || ' minutes')::interval,
         now() - interval '1 minute'
  FROM generate_series(1, ${total}) AS g;
  ANALYZE work_items;
END
\$\$;
SQL

echo "synthetic work_items ready: total=${total} pending=${pending}"
