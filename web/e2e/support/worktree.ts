import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

/** Runs a deterministic fixture/query against this journey's private worktree database. */
export function worktreeSQL(sql: string): string {
  const values = new Map(
    readFileSync(new URL('../../../.env', import.meta.url), 'utf8')
      .split('\n')
      .filter((line) => /^[A-Z_]+=/.test(line))
      .map((line) => {
        const equals = line.indexOf('=');
        return [line.slice(0, equals), line.slice(equals + 1).replace(/^(['"])(.*)\1$/, '$2')];
      }),
  );
  const database = values.get('DB_DATABASE') ?? '';
  if (!/^xflickr_wt[a-z0-9]+$/.test(database)) throw new Error('E2E requires a worktree database');
  return execFileSync(
    'psql',
    [
      '-X',
      '-A',
      '-t',
      '-q',
      '-v',
      'ON_ERROR_STOP=1',
      '-h',
      values.get('DB_HOST') ?? '127.0.0.1',
      '-p',
      values.get('DB_PORT') ?? '15435',
      '-U',
      values.get('DB_USERNAME') ?? '',
      '-d',
      database,
      '-c',
      sql,
    ],
    { env: { ...process.env, PGPASSWORD: values.get('DB_PASSWORD') ?? '' }, encoding: 'utf8' },
  ).trim();
}
