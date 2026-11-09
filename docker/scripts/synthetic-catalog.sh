#!/usr/bin/env bash
# Generate a synthetic catalog for partition-key EXPLAIN measurements.
# Not used by CI. Requires the worktree .env and refuses live databases.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if [[ ! -f .env ]]; then
  echo "synthetic-catalog: missing .env" >&2
  exit 1
fi

while IFS= read -r line || [[ -n "$line" ]]; do
  case "$line" in
    ''|\#*) continue ;;
  esac
  key="${line%%=*}"
  value="${line#*=}"
  export "$key=$value"
done < .env

DB_NAME="${DB_NAME:-${DB_DATABASE:-}}"
case "$DB_NAME" in
  xflickr|xflickr_test|'')
    echo "synthetic-catalog: refusing database '$DB_NAME'" >&2
    exit 1
    ;;
  xflickr_wt*)
    ;;
  *)
    echo "synthetic-catalog: unexpected database '$DB_NAME'" >&2
    exit 1
    ;;
esac

OWNERS="${OWNERS:-100000}"
PHOTOS="${PHOTOS:-3000000}"
FAVORITES="${FAVORITES:-3000000}"
BATCH="${BATCH:-250000}"

export PGPASSWORD="${DB_PASSWORD}"
PSQL=(psql -h "${DB_HOST:-127.0.0.1}" -p "${DB_PORT:-15435}" -U "${DB_USER:-${DB_USERNAME}}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 -X -q)

echo "synthetic-catalog: database=$DB_NAME owners=$OWNERS photos=$PHOTOS favorites=$FAVORITES batch=$BATCH"

echo "synthetic-catalog: session tunables + clear catalog"
"${PSQL[@]}" <<'SQL'
SET synchronous_commit = off;
SET work_mem = '256MB';
SET maintenance_work_mem = '1GB';
DROP TABLE IF EXISTS favorites_by_person CASCADE;
DROP TABLE IF EXISTS photos_by_owner CASCADE;
TRUNCATE favorites CASCADE;
TRUNCATE photos CASCADE;
TRUNCATE people CASCADE;
SQL

echo "synthetic-catalog: seeding people"
"${PSQL[@]}" <<SQL
SET synchronous_commit = off;
INSERT INTO people (nsid, username, realname)
SELECT
  'owner-' || lpad(g::text, 6, '0'),
  'user' || g::text,
  'Owner ' || g::text
FROM generate_series(0, ${OWNERS} - 1) AS g;
SQL

echo "synthetic-catalog: seeding photos in batches"
start=1
while (( start <= PHOTOS )); do
  stop=$(( start + BATCH - 1 ))
  if (( stop > PHOTOS )); then stop=$PHOTOS; fi
  echo "  photos ${start}..${stop}"
  "${PSQL[@]}" <<SQL
SET synchronous_commit = off;
INSERT INTO photos (
  id, owner_nsid, secret, server, title, last_update, visibility,
  raw, content_hash, uploaded_at, last_seen_at
)
SELECT
  g,
  'owner-' || lpad((g % ${OWNERS})::text, 6, '0'),
  'sec', '1', 'photo ' || g::text,
  timestamptz '2024-01-01' + ((g % 700) || ' days')::interval,
  'public', '{}'::jsonb,
  decode(lpad(to_hex(g), 64, '0'), 'hex'),
  timestamptz '2020-01-01' + ((g % 2000) || ' days')::interval,
  timestamptz '2024-01-01' + ((g % 700) || ' days')::interval
FROM generate_series(${start}, ${stop}) AS g;
SQL
  start=$(( stop + 1 ))
done

echo "synthetic-catalog: seeding favorites in batches"
start=1
while (( start <= FAVORITES )); do
  stop=$(( start + BATCH - 1 ))
  if (( stop > FAVORITES )); then stop=$FAVORITES; fi
  echo "  favorites ${start}..${stop}"
  "${PSQL[@]}" <<SQL
SET synchronous_commit = off;
INSERT INTO favorites (person_nsid, photo_id, fave_date, seen_at)
SELECT
  'owner-' || lpad((g % ${OWNERS})::text, 6, '0'),
  ((g % ${PHOTOS}) + 1),
  timestamptz '2021-01-01' + ((g % 1500) || ' days')::interval,
  timestamptz '2024-06-01' + ((g % 200) || ' days')::interval
FROM generate_series(${start}, ${stop}) AS g
ON CONFLICT DO NOTHING;
SQL
  start=$(( stop + 1 ))
done

echo "synthetic-catalog: building HASH(owner_nsid) comparison copy"
COPY_START=$(date +%s)
"${PSQL[@]}" <<'SQL'
SET synchronous_commit = off;
SET maintenance_work_mem = '1GB';
CREATE TABLE photos_by_owner (
  LIKE photos INCLUDING DEFAULTS
) PARTITION BY HASH (owner_nsid);

DO $$
DECLARE i int;
BEGIN
  FOR i IN 0..15 LOOP
    EXECUTE format(
      'CREATE TABLE photos_by_owner_p%s PARTITION OF photos_by_owner FOR VALUES WITH (MODULUS 16, REMAINDER %s)',
      lpad(i::text, 2, '0'), i
    );
  END LOOP;
END$$;

ALTER TABLE photos_by_owner ADD PRIMARY KEY (owner_nsid, id);
CREATE INDEX photos_by_owner_uploaded_idx
  ON photos_by_owner (owner_nsid, uploaded_at DESC NULLS LAST, id DESC);

INSERT INTO photos_by_owner SELECT * FROM photos;

CREATE TABLE favorites_by_person (
  LIKE favorites INCLUDING DEFAULTS
) PARTITION BY HASH (person_nsid);

DO $$
DECLARE i int;
BEGIN
  FOR i IN 0..15 LOOP
    EXECUTE format(
      'CREATE TABLE favorites_by_person_p%s PARTITION OF favorites_by_person FOR VALUES WITH (MODULUS 16, REMAINDER %s)',
      lpad(i::text, 2, '0'), i
    );
  END LOOP;
END$$;

ALTER TABLE favorites_by_person ADD PRIMARY KEY (person_nsid, photo_id);
CREATE INDEX favorites_by_person_fave_idx
  ON favorites_by_person (person_nsid, fave_date DESC, photo_id DESC)
  WHERE removed_at IS NULL;

INSERT INTO favorites_by_person SELECT * FROM favorites;
SQL
COPY_END=$(date +%s)
echo "synthetic-catalog: comparison copy wall_seconds=$((COPY_END - COPY_START))"

echo "synthetic-catalog: ANALYZE"
"${PSQL[@]}" -c 'ANALYZE people; ANALYZE photos; ANALYZE favorites; ANALYZE photos_by_owner; ANALYZE favorites_by_person;'

echo "synthetic-catalog: row counts"
"${PSQL[@]}" -c "SELECT 'people' AS t, count(*) FROM people UNION ALL SELECT 'photos', count(*) FROM photos UNION ALL SELECT 'favorites', count(*) FROM favorites UNION ALL SELECT 'photos_by_owner', count(*) FROM photos_by_owner UNION ALL SELECT 'favorites_by_person', count(*) FROM favorites_by_person;"

echo "synthetic-catalog: done"
