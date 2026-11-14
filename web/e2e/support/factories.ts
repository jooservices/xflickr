import { worktreeSQL } from './worktree';

/** Data factories: seed and query this journey's private worktree database. */

export type Incident = { id: string; kind: string; scope: string; unsafeMarker: string };

/** An open critical incident whose detail holds a private marker the admin screens must never show. */
export function insertIncident(marker: string): Incident {
  const kind = `e2e.issues.${marker}`;
  const scope = `e2e:${marker}`;
  const unsafeMarker = `private-${marker}`;
  const id = worktreeSQL(`
    INSERT INTO incidents (kind, scope, severity, title, detail, first_seen, last_seen, count)
    VALUES ('${kind}', '${scope}', 'critical', '${unsafeMarker}',
      '{"evidence":"${unsafeMarker}"}'::jsonb, now(), now() + interval '5 minutes', 3)
    RETURNING id;
  `);
  return { id, kind, scope, unsafeMarker };
}

export function deleteIncident(incident: Incident): void {
  worktreeSQL(
    `DELETE FROM incidents WHERE kind = '${incident.kind}' AND scope = '${incident.scope}';`,
  );
}

/** A person the crawl will find missing: an interest and an albums work item for an account that has none of it. */
export function seedMissingPerson(nsid: string): void {
  worktreeSQL(`INSERT INTO crawl_interests(user_id,person_nsid,depth,via)
    SELECT user_id,'${nsid}',0,'e2e' FROM flickr_accounts LIMIT 1
    ON CONFLICT DO NOTHING;
    INSERT INTO work_items(kind,subject_key,user_id,key_id,depth,cursor)
    SELECT 'albums','${nsid}',user_id,key_id,0,
      '{"page":1,"method":"flickr.photosets.getList","params":{"user_id":"${nsid}","per_page":"500"}}'
    FROM flickr_accounts LIMIT 1 ON CONFLICT DO NOTHING;`);
}

export const personStateCount = (nsid: string): string =>
  worktreeSQL(
    `SELECT count(*) FROM entity_states WHERE entity_type='person' AND entity_id='${nsid}'`,
  );

export const personFirstSeen = (nsid: string): string =>
  worktreeSQL(
    `SELECT first_seen_at::text FROM entity_states WHERE entity_type='person' AND entity_id='${nsid}'`,
  );

export function requeueAlbums(nsid: string): void {
  worktreeSQL(`UPDATE work_items SET state='pending',not_before=now(),attempts=0
    WHERE kind='albums' AND subject_key='${nsid}'`);
}

export const albumsState = (nsid: string): string =>
  worktreeSQL(`SELECT state FROM work_items WHERE kind='albums' AND subject_key='${nsid}'`);

export const downloadItemIsActive = (downloadID: number, photoID: number): string =>
  worktreeSQL(
    `SELECT state IN ('pending','fetching','ready') FROM download_items WHERE download_id=${downloadID} AND photo_id=${photoID}`,
  );

export const mediaFetchJobCount = (downloadID: number): string =>
  worktreeSQL(
    `SELECT count(*) FROM river_job WHERE args->>'kind'='media.fetch' AND args->'args'->>'download_id'='${downloadID}'`,
  );

/** The favorite library of a member: `total` favorites of the account, newest first, from three owners. */
export const LIBRARY_TOTAL = 501;
const LIBRARY_OWNERS = ['linh@N00', 'morgan@N00', 'new@N00'];

/**
 * Replaces the favorites of the account with `LIBRARY_TOTAL` visible ones (photo ids 1..total): owner linh@N00 for
 * photo 1, new@N00 for photos 500 and up, morgan@N00 for the rest. The member observed every owner and photo, so
 * the sharing rules show them all.
 */
export function seedFavoriteLibrary(accountNsid: string): void {
  const owners = LIBRARY_OWNERS.map((nsid) => `('${nsid}','${nsid}')`).join(',');
  const names = LIBRARY_OWNERS.map((nsid) => `'${nsid}'`).join(',');
  worktreeSQL(`
    DELETE FROM favorites WHERE person_nsid = '${accountNsid}';
    INSERT INTO people(nsid, username) VALUES ${owners} ON CONFLICT DO NOTHING;
    INSERT INTO photos(id, owner_nsid, title, last_update, visibility, sizes_status, sizes, raw, content_hash)
      SELECT g, CASE WHEN g >= 500 THEN 'new@N00' WHEN g = 1 THEN 'linh@N00' ELSE 'morgan@N00' END,
             'Photo ' || g, now(), 'public', 'confirmed', '[]', '{}', decode(repeat('ab', 32), 'hex')
      FROM generate_series(1, ${LIBRARY_TOTAL}) AS g ON CONFLICT DO NOTHING;
    INSERT INTO favorites(person_nsid, photo_id, fave_date)
      SELECT '${accountNsid}', g, now() - (g || ' seconds')::interval FROM generate_series(1, ${LIBRARY_TOTAL}) AS g;
    INSERT INTO observations(user_id, flickr_account_id, record_kind, record_id)
      SELECT fa.user_id, fa.id, 'person', p FROM flickr_accounts fa, unnest(ARRAY[${names}]) AS p
      WHERE fa.nsid = '${accountNsid}' ON CONFLICT DO NOTHING;
    INSERT INTO observations(user_id, flickr_account_id, record_kind, record_id)
      SELECT fa.user_id, fa.id, 'photo', g::text FROM flickr_accounts fa, generate_series(1, ${LIBRARY_TOTAL}) AS g
      WHERE fa.nsid = '${accountNsid}' ON CONFLICT DO NOTHING;
  `);
}

export function removeFavoriteLibrary(accountNsid: string): void {
  worktreeSQL(`
    DELETE FROM favorites WHERE person_nsid = '${accountNsid}' AND photo_id BETWEEN 1 AND ${LIBRARY_TOTAL};
    DELETE FROM observations WHERE record_kind = 'photo' AND record_id ~ '^[0-9]+$' AND record_id::bigint BETWEEN 1 AND ${LIBRARY_TOTAL};
    DELETE FROM photos WHERE id BETWEEN 1 AND ${LIBRARY_TOTAL};
  `);
}

export type PlanSeed = {
  name: string;
  weight: number;
  maxDepthCap: number;
  maxContacts: number;
  aiQuotaPerDay: number;
  zipTtlHours: number;
  resourceShare: number;
};

export function insertPlan(plan: PlanSeed): void {
  worktreeSQL(`INSERT INTO plans(name, weight, max_depth_cap, max_contacts, ai_quota_per_day, zip_ttl_hours, resource_share)
    VALUES ('${plan.name}', ${plan.weight}, ${plan.maxDepthCap}, ${plan.maxContacts}, ${plan.aiQuotaPerDay}, ${plan.zipTtlHours}, ${plan.resourceShare})`);
}

export function setPlanContacts(name: string, contacts: number): void {
  worktreeSQL(`UPDATE plans SET max_contacts = ${contacts} WHERE name = '${name}'`);
}

export function deletePlan(name: string): void {
  worktreeSQL(`DELETE FROM plans WHERE name = '${name}'`);
}

/** Changes a member's role; the session reads the role from the database on every request. */
export function setUserRole(email: string, role: 'user' | 'admin'): void {
  worktreeSQL(`UPDATE users SET role = '${role}' WHERE email = '${email}'`);
}

export type KeyRow = { id: number; userId: number; label: string };

/** The first API key of the database. */
export function firstKey(): KeyRow {
  const [id, userId, label] = worktreeSQL(
    `SELECT id, user_id, label FROM api_keys ORDER BY id LIMIT 1`,
  ).split('|');
  return { id: Number(id), userId: Number(userId), label: label ?? '' };
}

export function relabelKey(id: number, label: string): void {
  worktreeSQL(`UPDATE api_keys SET label = '${label}' WHERE id = ${id}`);
}
