import { useEffect, useMemo, useRef, useState } from 'react';
import { analyze } from './api';
import { COLUMNS, sortRows, type SortState, type ViewModes } from './columns';
import { ColumnPicker } from './components/ColumnPicker';
import { PortfolioTabs } from './components/PortfolioTabs';
import { MAX_WIDTH, MIN_WIDTH, ResultsTable } from './components/ResultsTable';
import { Segmented } from './components/Segmented';
import { StockSelect } from './components/StockSelect';
import { ThemeToggle } from './components/ThemeToggle';
import { copyForExcel } from './copyTable';
import { fmtDate, num } from './format';
import { loadPortfolios, MAX_STOCKS, newPortfolio, savePortfolios, type Portfolio } from './portfolios';
import type { ScanResult } from './types';

const HIDDEN_KEY = 'stockScanner.hiddenColumns';
const WIDTHS_KEY = 'stockScanner.columnWidths';

function loadHidden(): Set<string> {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(HIDDEN_KEY) ?? '[]');
    const known = new Set(COLUMNS.slice(1).map((c) => c.id));
    return new Set(Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === 'string' && known.has(x)) : []);
  } catch {
    return new Set();
  }
}

function saveHidden(v: Set<string>) {
  try {
    localStorage.setItem(HIDDEN_KEY, JSON.stringify([...v]));
  } catch {
    // storage unavailable
  }
}

function loadWidths(): Record<string, number> {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(WIDTHS_KEY) ?? '{}');
    const known = new Set(COLUMNS.map((c) => c.id));
    return Object.fromEntries(
      Object.entries(parsed && typeof parsed === 'object' ? parsed : {}).filter(
        ([id, w]) => known.has(id) && typeof w === 'number' && w >= MIN_WIDTH && w <= MAX_WIDTH,
      ),
    );
  } catch {
    return {};
  }
}

function saveWidths(v: Record<string, number>) {
  try {
    localStorage.setItem(WIDTHS_KEY, JSON.stringify(v));
  } catch {
    // storage unavailable
  }
}

export function App() {
  const [portfolios, setPortfolios] = useState(loadPortfolios);
  // Per portfolio id: the last scan, the scan in flight, and the last scan's error.
  const [results, setResults] = useState<Record<string, ScanResult>>({});
  const [scanning, setScanning] = useState<Set<string>>(new Set());
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [sort, setSort] = useState<SortState>({ id: null, dir: 1 });
  const [modes, setModes] = useState<ViewModes>({ sma: 'dist', pivot: 'dist' });

  const [hidden, setHidden] = useState<Set<string>>(loadHidden);

  const active = portfolios.list.find((p) => p.id === portfolios.activeId) ?? portfolios.list[0];
  const selected = active.stocks;
  const result = results[active.id] ?? null;
  const loading = scanning.has(active.id);
  const error = errors[active.id] ?? '';

  useEffect(() => savePortfolios(portfolios), [portfolios]);

  const tableRef = useRef<HTMLTableElement>(null);
  const [copied, setCopied] = useState(false);

  async function copyTable() {
    if (!tableRef.current || !result) return;
    try {
      await copyForExcel(tableRef.current, `${active.name} · data as of ${fmtDate(result.benchmark.date)}`);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (e) {
      setError(active.id, `Couldn't copy the table: ${(e as Error).message}`);
    }
  }
  useEffect(() => saveHidden(hidden), [hidden]);

  const [widths, setWidths] = useState<Record<string, number>>(loadWidths);
  useEffect(() => saveWidths(widths), [widths]);

  function onResize(id: string, width: number | null) {
    setWidths(({ [id]: _, ...rest }) => (width == null ? rest : { ...rest, [id]: width }));
  }

  const columns = useMemo(() => COLUMNS.filter((c) => !hidden.has(c.id)), [hidden]);

  const selectedKeys = useMemo(() => new Set(selected.map((s) => s.toUpperCase())), [selected]);
  // Rows and problems for stocks removed since the last scan drop out right away.
  const rows = useMemo(
    () => (result ? sortRows(result.rows.filter((r) => selectedKeys.has(r.query.toUpperCase())), sort, modes) : []),
    [result, selectedKeys, sort, modes],
  );
  const problems = useMemo(() => (result?.problems ?? []).filter((p) => selectedKeys.has(p.query.toUpperCase())), [result, selectedKeys]);
  /** Selected entries without a result row: skipped by the last scan, or added since. */
  const pending = useMemo(() => {
    const done = new Set(rows.map((r) => r.query.toUpperCase()));
    const reasons = new Map(problems.map((p) => [p.query.toUpperCase(), p.reason]));
    return selected
      .filter((s) => !done.has(s.toUpperCase()))
      .map((entry) => ({ entry, reason: reasons.get(entry.toUpperCase()) }));
  }, [selected, rows, problems]);

  /** Edits the open portfolio's stocks. */
  function setSelected(next: string[] | ((s: string[]) => string[])) {
    setPortfolios((ps) => ({
      ...ps,
      list: ps.list.map((p) => (p.id === ps.activeId ? { ...p, stocks: typeof next === 'function' ? next(p.stocks) : next } : p)),
    }));
  }

  function removeStock(entry: string) {
    const key = entry.toUpperCase();
    setSelected((s) => s.filter((x) => x.toUpperCase() !== key));
  }

  const setError = (id: string, message: string) => setErrors((m) => ({ ...m, [id]: message }));

  async function scan(p: Portfolio) {
    setError(p.id, '');
    if (!p.stocks.length) {
      setError(p.id, 'Add at least one stock.');
      return;
    }
    setScanning((s) => new Set(s).add(p.id));
    try {
      const r = await analyze(p.stocks);
      setResults((m) => ({ ...m, [p.id]: r }));
    } catch (e) {
      setError(p.id, (e as Error).message); // the previous table stays
    } finally {
      setScanning((s) => {
        const next = new Set(s);
        next.delete(p.id);
        return next;
      });
    }
  }

  const runScan = () => scan(active);

  /** Shows a portfolio, analysing it first if it hasn't been this session. */
  function show(p: Portfolio) {
    if (p.stocks.length && !results[p.id] && !scanning.has(p.id)) scan(p);
  }

  // Analyse the open portfolio on page load (once, even under StrictMode's double effects).
  const loaded = useRef(false);
  useEffect(() => {
    if (loaded.current) return;
    loaded.current = true;
    show(active);
  });

  function switchTo(id: string) {
    const p = portfolios.list.find((x) => x.id === id);
    if (!p) return;
    setPortfolios((ps) => ({ ...ps, activeId: id }));
    show(p);
  }

  function createPortfolio(name: string) {
    const p = newPortfolio(name);
    setPortfolios((ps) => ({ list: [...ps.list, p], activeId: p.id }));
  }

  function renamePortfolio(id: string, name: string) {
    setPortfolios((ps) => ({ ...ps, list: ps.list.map((p) => (p.id === id ? { ...p, name } : p)) }));
  }

  function deletePortfolio(id: string) {
    const i = portfolios.list.findIndex((p) => p.id === id);
    const list = portfolios.list.filter((p) => p.id !== id);
    if (i < 0 || !list.length) return;
    // Deleting the open portfolio opens its neighbour.
    const next = portfolios.activeId === id ? list[Math.min(i, list.length - 1)] : active;
    setPortfolios({ list, activeId: next.id });
    setResults(({ [id]: _, ...rest }) => rest);
    show(next);
  }


  function onHide(next: Set<string>) {
    setHidden(next);
    // Sorting by a column you can't see would be confusing.
    if (sort.id && next.has(sort.id)) setSort({ id: null, dir: 1 });
  }

  function onSort(id: string) {
    setSort((s) => {
      if (s.id === id) return { id, dir: s.dir === 1 ? -1 : 1 };
      return { id, dir: COLUMNS.find((c) => c.id === id)?.text ? 1 : -1 };
    });
  }

  const generated = result ? new Date(result.generated_at).toLocaleTimeString('en-IN', { hour: '2-digit', minute: '2-digit' }) : '';

  return (
    <main>
      <header className="top">
        <h1>Stock Scanner</h1>
        <ThemeToggle />
      </header>
      <p className="lede">
        Pick NSE stocks, or paste symbols, ISINs or company names, to get RS55, multi-timeframe RSI, SMAs, classic pivots and Weinstein
        stage in one table.
      </p>

      <PortfolioTabs
        list={portfolios.list}
        activeId={active.id}
        onSwitch={switchTo}
        onCreate={createPortfolio}
        onRename={renamePortfolio}
        onDelete={deletePortfolio}
      />

      <div className="input-row">
        <StockSelect selected={selected} onChange={setSelected} onSubmit={runScan} max={MAX_STOCKS} resetKey={active.id} />
        <div className="actions">
          <button className="primary" type="button" onClick={runScan} disabled={loading}>
            {loading ? 'Loading…' : 'Analyze'}
          </button>
          <button type="button" disabled={!rows.length} onClick={copyTable} title="Copy the table as shown, with its colours, to paste into Excel">
            {copied ? 'Copied ✓' : 'Copy for Excel'}
          </button>
        </div>
      </div>
      <p className="error" role="alert">
        {error}
      </p>

      {result && result.notices.length > 0 && (
        <div className="strip notices">
          {result.notices.map((n) => (
            <div key={n}>{n}</div>
          ))}
        </div>
      )}
      {problems.length > 0 && (
        <div className="strip problems">
          <strong>
            Skipped {problems.length} {problems.length === 1 ? 'entry' : 'entries'}:
          </strong>
          <ul>
            {problems.map((p) => (
              <li key={p.query}>
                <b>{p.query}</b> — {p.reason}
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="toolbar">
        <ColumnPicker hidden={hidden} onChange={onHide} resized={Object.keys(widths).length > 0} onResetWidths={() => setWidths({})} />
        <Segmented
          label="SMA"
          value={modes.sma}
          options={[
            ['dist', '% from price'],
            ['value', 'Value'],
          ]}
          onChange={(sma) => setModes((m) => ({ ...m, sma }))}
        />
        <Segmented
          label="Pivot & VWAP"
          value={modes.pivot}
          options={[
            ['dist', '% from level'],
            ['value', 'Level'],
          ]}
          onChange={(pivot) => setModes((m) => ({ ...m, pivot }))}
        />
        {result && (
          <div className="data-info">
            {result.benchmark.name} {num(result.benchmark.close)} · data as of {fmtDate(result.benchmark.date)} · {rows.length} stocks ·
            generated {generated}
          </div>
        )}
      </div>

      <ResultsTable tableRef={tableRef} columns={columns} widths={widths} onResize={onResize} onHide={(ids) => onHide(new Set([...hidden, ...ids]))} rows={rows} pending={pending} sort={sort} modes={modes} onSort={onSort} onRemove={removeStock} />

      <div className="legend">
        <p>
          <b>RS55</b> = (stock close ÷ close 55 sessions ago) ÷ (Nifty 50 close ÷ close 55 sessions ago) − 1; above 0 beats the index; arrow
          compares with 5 sessions ago.
        </p>
        <p>
          <b>RSI (14)</b> Wilder smoothing on daily, weekly (Friday) and monthly closes. 70+ hot · 60–70 strong · 40–60 neutral · 30–40 weak ·
          30 and below cold.
        </p>
        <p>
          <b>SMA</b> plain mean of the last N daily closes; distance = (close ÷ SMA − 1) × 100.
        </p>
        <p>
          <b>Classic pivot</b> P = (H + L + C) ÷ 3 of the previous completed day, week or month; distance = (close ÷ P − 1) × 100. Hover for
          R1–R3 and S1–S3.
        </p>
        <p>
          <b>VWAP</b> volume-weighted average of the latest session's 5-minute typical prices (H + L + C) ÷ 3; distance = (close ÷ VWAP −
          1) × 100.
        </p>
        <p>
          <b>Stage</b> 30-week SMA of weekly closes and its 4-week slope (±1% is flat); 5% band for pullbacks and bounces. Hover for the
          reasoning.
        </p>
      </div>
    </main>
  );
}
