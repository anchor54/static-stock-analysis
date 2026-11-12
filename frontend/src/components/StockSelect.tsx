import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react';
import { fetchSymbols } from '../api';
import { cx, splitEntries } from '../format';

interface Indexed {
  symbol: string;
  name: string;
  isin: string;
  s: string; // lower-cased symbol
  n: string; // lower-cased name
  i: string; // lower-cased ISIN
}

const MAX_RESULTS = 50;
const SEPARATORS = /[,;\r\n]/;

/** Ranks symbol and ISIN matches above name matches, and prefixes above substrings. */
function findStocks(list: Indexed[], query: string): Indexed[] {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  const hits: [number, Indexed][] = [];
  for (const e of list) {
    let rank: number;
    if (e.s === q || e.i === q) rank = 0;
    else if (e.s.startsWith(q)) rank = 1;
    else if (e.n.startsWith(q)) rank = 2;
    else if (e.n.includes(` ${q}`)) rank = 3;
    else if (e.s.includes(q) || e.n.includes(q)) rank = 4;
    else continue;
    hits.push([rank, e]);
  }
  hits.sort((a, b) => a[0] - b[0] || a[1].s.length - b[1].s.length || a[1].s.localeCompare(b[1].s));
  return hits.slice(0, MAX_RESULTS).map((h) => h[1]);
}

function Highlight({ text, query }: { text: string; query: string }): ReactNode {
  const q = query.trim().toLowerCase();
  const i = q ? text.toLowerCase().indexOf(q) : -1;
  if (i < 0) return text;
  return (
    <>
      {text.slice(0, i)}
      <mark>{text.slice(i, i + q.length)}</mark>
      {text.slice(i + q.length)}
    </>
  );
}

interface Props {
  /** Selected entries: NSE symbols, or free text (names, ISINs, Yahoo tickers) for the server to resolve. */
  selected: string[];
  onChange(next: string[]): void;
  /** Run the scan (Enter on an empty search, or Ctrl+Enter). */
  onSubmit(): void;
  max: number;
  /** Changes when a different portfolio opens; clears the search and its status note. */
  resetKey: string;
}

/** Search box over the NSE equity list that adds stocks to the selection; stocks are removed from the results table. */
export function StockSelect({ selected, onChange, onSubmit, max, resetKey }: Props) {
  const id = useId();
  const inputId = `${id}-input`;
  const listId = `${id}-list`;
  const optId = (i: number) => `${id}-opt-${i}`;

  const [list, setList] = useState<Indexed[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [listStatus, setListStatus] = useState('');
  const [message, setMessage] = useState('');
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const loadStarted = useRef(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const load = useCallback(() => {
    if (loadStarted.current) return;
    loadStarted.current = true;
    setLoading(true);
    fetchSymbols()
      .then((d) => {
        setList(d.symbols.map((e) => ({ ...e, s: e.symbol.toLowerCase(), n: e.name.toLowerCase(), i: (e.isin || '').toLowerCase() })));
        setListStatus(`${d.symbols.length.toLocaleString('en-IN')} NSE stocks${d.stale ? ' (list may be out of date)' : ''}`);
      })
      .catch((e: Error) => {
        loadStarted.current = false;
        setListStatus(e.message);
      })
      .finally(() => setLoading(false));
  }, []);

  // Load the list up front so the first search is instant.
  useEffect(load, [load]);

  useEffect(() => {
    setQuery('');
    setMessage('');
  }, [resetKey]);

  /** Upper-cased symbol or ISIN → stock. */
  const lookup = useMemo(() => {
    const m = new Map<string, Indexed>();
    for (const e of list ?? []) {
      m.set(e.symbol.toUpperCase(), e);
      if (e.isin) m.set(e.isin.toUpperCase(), e);
    }
    return m;
  }, [list]);

  const selectedKeys = useMemo(() => new Set(selected.map((s) => s.toUpperCase())), [selected]);
  const results = useMemo(() => (list ? findStocks(list, query) : []), [list, query]);
  const showList = open && query.trim() !== '';

  useEffect(() => setActive(results.length ? 0 : -1), [results]);
  useEffect(() => {
    if (active >= 0) document.getElementById(`${id}-opt-${active}`)?.scrollIntoView({ block: 'nearest' });
  }, [id, active]);

  /** Adds entries; an exact symbol or ISIN becomes the NSE symbol, anything else is kept as typed. */
  function add(entries: string[]) {
    const next = [...selected];
    const keys = new Set(selectedKeys);
    const added: string[] = [];
    let dup = 0;
    let over = 0;
    for (const raw of entries) {
      const entry = lookup.get(raw.toUpperCase())?.symbol ?? raw;
      const key = entry.toUpperCase();
      if (keys.has(key)) dup++;
      else if (next.length >= max) over++;
      else {
        keys.add(key);
        next.push(entry);
        added.push(entry);
      }
    }
    if (added.length) onChange(next);
    const parts = [];
    if (added.length) parts.push(added.length === 1 ? `Added ${added[0]}` : `Added ${added.length} stocks`);
    if (dup) parts.push(dup === 1 && entries.length === 1 ? `${entries[0].toUpperCase()} is already selected` : `${dup} already selected`);
    if (over) parts.push(`${over} not added: the limit is ${max}`);
    setMessage(parts.join(' · '));
  }

  function pick(i: number) {
    const e = results[i];
    if (!e) return;
    add([e.symbol]);
    setQuery('');
    setOpen(false);
  }

  function commitTyped(text: string) {
    const entries = splitEntries(text);
    if (entries.length) add(entries);
    setQuery('');
    setOpen(false);
  }

  function onInputChange(value: string) {
    // Typing a comma, semicolon or new line commits what came before it.
    if (SEPARATORS.test(value)) {
      const parts = value.split(SEPARATORS);
      const rest = parts.pop() ?? '';
      add(splitEntries(parts.join(',')));
      setQuery(rest.trimStart());
    } else {
      setQuery(value);
    }
    setOpen(true);
  }

  function onPaste(e: ClipboardEvent<HTMLInputElement>) {
    const text = e.clipboardData.getData('text');
    if (SEPARATORS.test(text)) {
      e.preventDefault(); // a text input would drop the line breaks
      commitTyped(query + text);
    }
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!showList) {
        setOpen(true);
        return;
      }
      const n = results.length;
      if (n) setActive((a) => (a + (e.key === 'ArrowDown' ? 1 : -1) + n) % n);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (e.ctrlKey || e.metaKey) onSubmit();
      else if (showList && active >= 0) pick(active);
      else if (query.trim()) commitTyped(query); // free text: a company name, ISIN or Yahoo ticker
      else onSubmit();
    } else if (e.key === 'Escape') {
      if (showList) {
        e.preventDefault();
        setOpen(false);
      } else {
        setQuery('');
      }
    }
  }

  let options: ReactNode = null;
  if (showList) {
    if (!list) {
      options = (
        <li className="empty">{loading ? 'Loading stock list…' : 'Stock list unavailable. Press Enter to add what you typed.'}</li>
      );
    } else if (!results.length) {
      options = (
        <li className="empty">
          No matching NSE stocks. Press Enter to add “{query.trim()}” as typed.
        </li>
      );
    } else {
      options = results.map((e, i) => {
        const isSelected = selectedKeys.has(e.symbol.toUpperCase());
        return (
          <li
            key={e.symbol}
            id={optId(i)}
            role="option"
            aria-selected={isSelected}
            className={cx(i === active && 'active')}
            onMouseDown={(ev) => ev.preventDefault() /* keep focus in the search box */}
            onMouseEnter={() => setActive(i)}
            onClick={() => pick(i)}
          >
            <span className="o-check" aria-hidden="true">
              {isSelected ? '✓' : ''}
            </span>
            <span className="o-sym">
              <Highlight text={e.symbol} query={query} />
            </span>
            <span className="o-name" title={e.name}>
              <Highlight text={e.name} query={query} />
            </span>
            <span className="o-isin">{e.isin}</span>
          </li>
        );
      });
    }
  }

  return (
    <div className="stock-select">
      <label htmlFor={inputId}>Stocks</label>
      <div className="combo">
        <div className="search-box">
          <input
            ref={inputRef}
            id={inputId}
            type="text"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={showList}
            aria-controls={listId}
            aria-activedescendant={showList && active >= 0 ? optId(active) : undefined}
            aria-describedby={`${id}-status`}
            autoComplete="off"
            spellCheck={false}
            placeholder="Search by symbol, company name or ISIN, or paste a list…"
            value={query}
            onFocus={load}
            onChange={(e) => onInputChange(e.target.value)}
            onPaste={onPaste}
            onKeyDown={onKeyDown}
            onBlur={() => setOpen(false)}
          />
        </div>
        {showList && (
          <ul id={listId} role="listbox" aria-label="Matching stocks" aria-multiselectable="true">
            {options}
          </ul>
        )}
      </div>
      <div className="select-status" id={`${id}-status`}>
        <span>
          {selected.length} of {max} selected
        </span>
        {listStatus && <span>· {listStatus}</span>}
        <span aria-live="polite">{message && `· ${message}`}</span>
        {selected.length > 0 && (
          <button
            type="button"
            className="link"
            onClick={() => {
              onChange([]);
              setMessage('Cleared all stocks');
              inputRef.current?.focus();
            }}
          >
            Clear all
          </button>
        )}
      </div>
    </div>
  );
}
