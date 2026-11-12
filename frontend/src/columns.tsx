import type { ReactNode } from 'react';
import { dirClass, fmtDate, isNum, num, pct, rsiBand, signed } from './format';
import type { PivotKey, RsiKey, Row, SmaPeriod } from './types';

export type Mode = 'dist' | 'value';
export interface ViewModes {
  sma: Mode;
  pivot: Mode;
}

export interface Cell {
  content: ReactNode;
  className?: string;
  title?: string;
}

export interface Column {
  id: string;
  group: string;
  label: string;
  align?: 'left';
  className?: string;
  /** Text columns sort A→Z first; numeric columns sort high→low first. */
  text?: boolean;
  value(row: Row, modes: ViewModes): number | string | null;
  cell(row: Row, modes: ViewModes): Cell;
}

export const SMA_PERIODS = [9, 20, 50, 200] as const satisfies readonly SmaPeriod[];
export const PIVOTS = [
  ['d', 'Day', 'daily'],
  ['w', 'Week', 'weekly'],
  ['m', 'Month', 'monthly'],
] as const satisfies readonly (readonly [PivotKey, string, string])[];
const RSIS = [
  ['rsi_d', 'D', 'Daily'],
  ['rsi_w', 'W', 'Weekly'],
  ['rsi_m', 'M', 'Monthly'],
] as const satisfies readonly (readonly [RsiKey, string, string])[];

const dash = <span className="missing">–</span>;

function WarnIcon({ row }: { row: Row }) {
  const notes = [row.note, ...row.warnings].filter(Boolean);
  if (!notes.length) return null;
  return (
    <span className="warn" title={notes.join('\n')} tabIndex={0} role="img" aria-label={`Notes: ${notes.join(' ')}`}>
      ⚠
    </span>
  );
}

function rsCell(r: Row): Cell {
  if (!isNum(r.rs55)) return { content: dash };
  let arrow = '';
  if (isNum(r.rs55_5d_ago)) arrow = r.rs55 > r.rs55_5d_ago ? ' ▲' : r.rs55 < r.rs55_5d_ago ? ' ▼' : ' ▶';
  return {
    title: `RS55 now ${signed(r.rs55)}; 5 sessions ago ${signed(r.rs55_5d_ago)}`,
    content: <span className={`chip ${r.rs55 >= 0 ? 'pos' : 'neg'}`}>{signed(r.rs55) + arrow}</span>,
  };
}

function smaCell(r: Row, n: SmaPeriod, m: ViewModes): Cell {
  const v = r[`sma${n}`];
  const d = r[`sma${n}_dist`];
  if (!isNum(v)) return { content: '–', className: 'missing' };
  return m.sma === 'dist'
    ? { content: pct(d), className: dirClass(d), title: `SMA ${n}: ${num(v)}` }
    : { content: num(v), className: dirClass(d), title: `Price is ${pct(d)} from SMA ${n}` };
}

function pivotCell(r: Row, k: PivotKey, long: string, m: ViewModes): Cell {
  const p = r[`pivot_${k}`];
  const d = r[`pivot_${k}_dist`];
  if (!p) return { content: '–', className: 'missing' };
  const title = [
    `P ${num(p.p)}`,
    `R1 ${num(p.r1)}   S1 ${num(p.s1)}`,
    `R2 ${num(p.r2)}   S2 ${num(p.s2)}`,
    `R3 ${num(p.r3)}   S3 ${num(p.s3)}`,
    `From the ${long} bar ending ${fmtDate(p.from)}`,
    m.pivot === 'dist' ? '' : `Price is ${pct(d)} from P`,
  ]
    .filter(Boolean)
    .join('\n');
  return { content: m.pivot === 'dist' ? pct(d) : num(p.p), className: dirClass(d), title };
}

/** Follows the pivot toggle: % from VWAP, or the VWAP level. */
function vwapCell(r: Row, m: ViewModes): Cell {
  if (!isNum(r.vwap)) return { content: '–', className: 'missing', title: 'Daily VWAP unavailable; see the ⚠ note' };
  const session = `${fmtDate(r.vwap_date)} session`;
  return m.pivot === 'dist'
    ? { content: pct(r.vwap_dist), className: dirClass(r.vwap_dist), title: `VWAP ${num(r.vwap)} · ${session}` }
    : { content: num(r.vwap), className: dirClass(r.vwap_dist), title: `Price is ${pct(r.vwap_dist)} from VWAP · ${session}` };
}

export const COLUMNS: Column[] = [
  {
    id: 'symbol',
    group: 'Stock',
    label: 'Symbol',
    align: 'left',
    className: 'sym',
    text: true,
    value: (r) => r.symbol,
    cell: (r) => ({
      title: r.ticker + (r.query && r.query.toUpperCase() !== r.symbol ? ` (from “${r.query}”)` : ''),
      content: (
        <>
          {r.symbol}
          <WarnIcon row={r} />
        </>
      ),
    }),
  },
  {
    id: 'name',
    group: 'Stock',
    label: 'Company',
    align: 'left',
    className: 'name',
    text: true,
    value: (r) => r.name,
    cell: (r) => ({ content: r.name || '–', title: r.name }),
  },
  {
    id: 'close',
    group: 'Price',
    label: 'Close',
    value: (r) => r.close,
    cell: (r) => ({ content: num(r.close), title: `As of ${fmtDate(r.date)}` }),
  },
  {
    id: 'change_pct',
    group: 'Price',
    label: 'Day %',
    value: (r) => r.change_pct,
    cell: (r) => ({ content: pct(r.change_pct, 2), className: dirClass(r.change_pct) }),
  },
  { id: 'rs55', group: 'Relative strength', label: 'RS55 vs Nifty 50', value: (r) => r.rs55, cell: rsCell },
  ...RSIS.map(
    ([key, label, long]): Column => ({
      id: key,
      group: 'RSI (14)',
      label,
      value: (r) => r[key],
      cell: (r) => ({
        title: `${long} RSI (14)`,
        content: isNum(r[key]) ? <span className={`rsi ${rsiBand(r[key])}`}>{r[key].toFixed(1)}</span> : dash,
      }),
    }),
  ),
  ...SMA_PERIODS.map(
    (n): Column => ({
      id: `sma${n}`,
      group: 'SMA',
      label: String(n),
      value: (r, m) => (m.sma === 'dist' ? r[`sma${n}_dist`] : r[`sma${n}`]),
      cell: (r, m) => smaCell(r, n, m),
    }),
  ),
  ...PIVOTS.map(
    ([k, label, long]): Column => ({
      id: `pivot_${k}`,
      group: 'Classic pivot',
      label,
      value: (r, m) => (m.pivot === 'dist' ? r[`pivot_${k}_dist`] : (r[`pivot_${k}`]?.p ?? null)),
      cell: (r, m) => pivotCell(r, k, long, m),
    }),
  ),
  {
    id: 'vwap',
    group: 'VWAP',
    label: 'Daily',
    value: (r, m) => (m.pivot === 'dist' ? r.vwap_dist : r.vwap),
    cell: vwapCell,
  },
  {
    id: 'stage',
    group: 'Stage',
    label: 'Weinstein',
    align: 'left',
    value: (r) => r.stage?.number ?? null,
    cell: (r) =>
      r.stage
        ? {
            content: (
              <span className={`badge s${r.stage.number}`} title={r.stage.reason} tabIndex={0}>
                {r.stage.number} {r.stage.label}
              </span>
            ),
          }
        : { content: '–', className: 'missing' },
  },
];

export interface SortState {
  id: string | null;
  dir: 1 | -1;
}

const isMissing = (v: unknown) => v == null || v === '' || (typeof v === 'number' && !Number.isFinite(v));

/** Sorts rows by a column; missing values always sort last. */
export function sortRows(rows: Row[], sort: SortState, modes: ViewModes): Row[] {
  const col = COLUMNS.find((c) => c.id === sort.id);
  if (!col) return rows;
  return [...rows].sort((a, b) => {
    const va = col.value(a, modes);
    const vb = col.value(b, modes);
    const ma = isMissing(va);
    const mb = isMissing(vb);
    if (ma || mb) return ma === mb ? 0 : ma ? 1 : -1;
    if (typeof va === 'string' && typeof vb === 'string') {
      return sort.dir * va.localeCompare(vb, 'en', { sensitivity: 'base' });
    }
    return sort.dir * ((va as number) - (vb as number));
  });
}
