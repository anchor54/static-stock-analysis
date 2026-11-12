// Shapes of the Go API responses (internal/scanner and internal/api).

export type SmaPeriod = 9 | 20 | 50 | 200;
export type PivotKey = 'd' | 'w' | 'm';
export type RsiKey = 'rsi_d' | 'rsi_w' | 'rsi_m';

export interface PivotLevels {
  p: number | null;
  r1: number | null;
  r2: number | null;
  r3: number | null;
  s1: number | null;
  s2: number | null;
  s3: number | null;
  from: string;
}

export interface Stage {
  number: 1 | 2 | 3 | 4;
  name: string;
  label: string;
  reason: string;
}

export type Row = {
  query: string;
  symbol: string;
  ticker: string;
  name: string;
  date: string;
  close: number | null;
  change_pct: number | null;
  rs55: number | null;
  rs55_5d_ago: number | null;
  /** Session VWAP from the latest session's 5-minute bars, and the close's % distance from it. */
  vwap: number | null;
  vwap_dist: number | null;
  vwap_date: string;
  stage: Stage | null;
  /** Annual cash flows in ₹ crore for the financial year ending `…_fy`, and the year before. */
  fcf: number | null;
  fcf_prev: number | null;
  fcf_fy: string;
  net_cash: number | null;
  net_cash_prev: number | null;
  net_cash_fy: string;
  /** Set when Yahoo reports in another currency and the values were converted to rupees. */
  cash_note: string;
  /** FII and DII holdings in % at the latest quarter end and the one before; `…_chg` is the change in percentage points. */
  fii: number | null;
  fii_prev: number | null;
  fii_chg: number | null;
  dii: number | null;
  dii_prev: number | null;
  dii_chg: number | null;
  shp_quarter: string;
  shp_prev_quarter: string;
  /** "NSE" when fetched live, or "snapshot of YYYY-MM-DD" from the saved file. */
  shp_source: string;
  warnings: string[];
  note: string;
  match: string;
  candidates: string[];
} & Record<RsiKey | `sma${SmaPeriod}` | `sma${SmaPeriod}_dist` | `pivot_${PivotKey}_dist`, number | null> &
  Record<`pivot_${PivotKey}`, PivotLevels | null>;

export interface Problem {
  query: string;
  reason: string;
}

export interface ScanResult {
  rows: Row[];
  problems: Problem[];
  benchmark: { ticker: string; name: string; close: number | null; date: string };
  notices: string[];
  generated_at: string;
}

export interface SymbolEntry {
  symbol: string;
  name: string;
  isin: string;
}

export interface SymbolList {
  symbols: SymbolEntry[];
  fetched_at: string;
  stale: boolean;
}
