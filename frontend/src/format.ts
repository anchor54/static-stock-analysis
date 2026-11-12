export const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);

export const num = (v: number | null | undefined, digits = 2) =>
  isNum(v) ? v.toLocaleString('en-IN', { minimumFractionDigits: digits, maximumFractionDigits: digits }) : '–';

export const pct = (v: number | null | undefined, digits = 1) =>
  isNum(v) ? `${v > 0 ? '+' : ''}${v.toFixed(digits)}%` : '–';

export const signed = (v: number | null | undefined, digits = 3) =>
  isNum(v) ? `${v > 0 ? '+' : ''}${v.toFixed(digits)}` : '–';

/** ₹ crore, whole numbers with Indian digit grouping, e.g. 69,197. */
export const crore = (v: number | null | undefined) =>
  isNum(v) ? Math.round(v).toLocaleString('en-IN', { maximumFractionDigits: 0 }) : '–';

/** Financial year label from its end date: 2026-03-31 → FY26. */
export function fy(date: string | undefined) {
  const y = date ? Number(date.slice(0, 4)) : NaN;
  if (!Number.isFinite(y)) return 'latest year';
  // A year ending in January–March is named for that year; one ending later, for the next.
  const month = Number(date!.slice(5, 7));
  return `FY${String(month <= 3 ? y : y + 1).slice(2)}`;
}

/** Quarter label from its end date: 2026-09-30 → Sep 2026. */
export function quarter(date: string | undefined) {
  if (!date) return '–';
  const d = new Date(`${date}T00:00:00`);
  return Number.isNaN(d.getTime()) ? date : d.toLocaleDateString('en-IN', { month: 'short', year: 'numeric' });
}

/** Green when price is above the level, red when below. */
export const dirClass = (v: number | null | undefined) =>
  !isNum(v) ? 'missing' : v > 0 ? 'up' : v < 0 ? 'down' : '';

export function fmtDate(s: string | undefined) {
  if (!s) return '–';
  const d = new Date(`${s}T00:00:00`);
  return Number.isNaN(d.getTime()) ? s : d.toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' });
}

/** RSI colour band: 70+ hot, 60–70 strong, 40–60 neutral, 30–40 weak, 30 and below cold. */
export function rsiBand(v: number | null) {
  if (!isNum(v)) return '';
  if (v >= 70) return 'hot';
  if (v >= 60) return 'strong';
  if (v >= 40) return '';
  if (v > 30) return 'weak';
  return 'cold';
}

export const cx = (...names: (string | false | null | undefined)[]) => names.filter(Boolean).join(' ') || undefined;

/**
 * Splits pasted or typed text the same way the server does: on commas, semicolons and new lines;
 * spaces stay inside names. Drops empty and case-insensitively duplicate entries.
 */
export function splitEntries(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of text.split(/[,;\r\n]+/)) {
    const entry = part.trim().replace(/\s+/g, ' ');
    const key = entry.toUpperCase();
    if (entry && !seen.has(key)) {
      seen.add(key);
      out.push(entry);
    }
  }
  return out;
}
