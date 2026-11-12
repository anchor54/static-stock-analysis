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
