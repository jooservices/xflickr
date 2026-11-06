import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

/** Gives the connected account visibility of a crawled fixture before exercising download requests. */
export function observeCrawledPhoto(photoID: number): boolean {
  if (!Number.isSafeInteger(photoID) || photoID <= 0) throw new Error('Invalid fixture photo ID');
  const config = new Map(
    readFileSync(new URL('../../../.env', import.meta.url), 'utf8')
      .split('\n')
      .filter((line) => line !== '' && !line.startsWith('#') && line.includes('='))
      .map((line) => {
        const separator = line.indexOf('=');
        return [line.slice(0, separator), line.slice(separator + 1).replace(/^['"]|['"]$/g, '')];
      }),
  );
  const database = process.env.DB_DATABASE ?? config.get('DB_DATABASE') ?? '';
  if (!database.startsWith('xflickr_wt'))
    throw new Error('E2E fixtures require a worktree database');
  const env = { ...process.env };
  for (const [pg, key] of Object.entries({
    PGHOST: 'DB_HOST',
    PGPORT: 'DB_PORT',
    PGUSER: 'DB_USERNAME',
    PGPASSWORD: 'DB_PASSWORD',
  })) {
    env[pg] = process.env[key] ?? config.get(key) ?? '';
  }
  env.PGDATABASE = database;
  // The fixture's owner must already have a connected account. Do not change
  // sharing settings or bypass the API's normal visibility checks.
  const sql = `WITH observed AS (
    INSERT INTO observations(user_id, flickr_account_id, record_kind, record_id)
    SELECT fa.user_id, fa.id, 'photo', p.id::text
    FROM photos p JOIN flickr_accounts fa ON fa.nsid=p.owner_nsid
    WHERE p.id=${photoID}
    ON CONFLICT DO NOTHING RETURNING record_id
  ) SELECT EXISTS(SELECT 1 FROM observed) OR EXISTS(
    SELECT 1 FROM observations WHERE record_kind='photo' AND record_id='${photoID}'
  )`;
  return (
    execFileSync('psql', ['-XAtq', '-v', 'ON_ERROR_STOP=1', '-c', sql], { env })
      .toString()
      .trim() === 't'
  );
}
