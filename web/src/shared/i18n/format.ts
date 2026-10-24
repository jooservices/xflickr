export type MessageParams = Record<string, string | number>;

type Locale = string;

/** Index of the brace that closes the one opened at `start`, or -1. */
function matchBrace(text: string, start: number): number {
  let depth = 0;
  for (let i = start; i < text.length; i += 1) {
    if (text[i] === '{') depth += 1;
    if (text[i] === '}') {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  return -1;
}

/** Parses `=0 {a} one {b} other {c}` into a selector -> body map. */
function parseCases(source: string): Map<string, string> {
  const cases = new Map<string, string>();
  let i = 0;
  while (i < source.length) {
    const open = source.indexOf('{', i);
    if (open === -1) break;
    const selector = source.slice(i, open).trim();
    const close = matchBrace(source, open);
    if (close === -1) break;
    cases.set(selector, source.slice(open + 1, close));
    i = close + 1;
  }
  return cases;
}

function formatPlural(
  argument: string,
  body: string,
  params: MessageParams,
  locale: Locale,
): string {
  const cases = parseCases(body);
  const raw = params[argument];
  const count = typeof raw === 'number' ? raw : Number(raw);
  const exact = cases.get(`=${String(count)}`);
  const category = new Intl.PluralRules(locale).select(count);
  const chosen = exact ?? cases.get(category) ?? cases.get('other') ?? '';
  const number = new Intl.NumberFormat(locale).format(count);
  return formatMessage(chosen.replaceAll('#', number), params, locale);
}

/**
 * Formats a catalog message. Supports named placeholders `{name}` and the ICU
 * `{count, plural, =0 {..} one {..} other {..}}` form with `#` for the count.
 * A placeholder without a value is left as written.
 */
export function formatMessage(
  message: string,
  params: MessageParams = {},
  locale: Locale = 'en',
): string {
  let out = '';
  let i = 0;
  while (i < message.length) {
    const open = message.indexOf('{', i);
    if (open === -1) {
      out += message.slice(i);
      break;
    }
    out += message.slice(i, open);
    const close = matchBrace(message, open);
    if (close === -1) {
      out += message.slice(open);
      break;
    }
    const inner = message.slice(open + 1, close);
    const parts = /^\s*([\w-]+)\s*,\s*plural\s*,([\s\S]*)$/.exec(inner);
    if (parts?.[1] !== undefined && parts[2] !== undefined) {
      out += formatPlural(parts[1], parts[2], params, locale);
    } else {
      const value = params[inner.trim()];
      out += value === undefined ? `{${inner}}` : String(value);
    }
    i = close + 1;
  }
  return out;
}
