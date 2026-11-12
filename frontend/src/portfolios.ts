import { splitEntries } from './format';

/** Most entries per scan (the API's limit). */
export const MAX_STOCKS = 200;

export interface Portfolio {
  id: string;
  name: string;
  /** NSE symbols, or free text (names, ISINs, Yahoo tickers) for the server to resolve. */
  stocks: string[];
}

export interface Portfolios {
  list: Portfolio[];
  activeId: string;
}

const STORAGE_KEY = 'stockScanner.portfolios';
// Older saves: one unnamed selection, and before that the comma-separated text box.
const SELECTION_KEY = 'stockScanner.selection';
const LEGACY_KEY = 'stockScanner.lastInput';

export const newPortfolio = (name: string, stocks: string[] = []): Portfolio => ({
  id: Date.now().toString(36) + Math.random().toString(36).slice(2, 8),
  name,
  stocks,
});

const strings = (v: unknown) => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);

function loadOldSelection(): string[] {
  const saved = localStorage.getItem(SELECTION_KEY);
  if (saved) return strings(JSON.parse(saved)).slice(0, MAX_STOCKS);
  return splitEntries(localStorage.getItem(LEGACY_KEY) ?? '').slice(0, MAX_STOCKS);
}

export function loadPortfolios(): Portfolios {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null') as Partial<Portfolios> | null;
    const list = (Array.isArray(parsed?.list) ? parsed.list : [])
      .filter((p): p is Portfolio => !!p && typeof p.id === 'string' && typeof p.name === 'string')
      .map((p) => ({ id: p.id, name: p.name, stocks: strings(p.stocks).slice(0, MAX_STOCKS) }));
    if (list.length) {
      const activeId = list.some((p) => p.id === parsed?.activeId) ? parsed!.activeId! : list[0].id;
      return { list, activeId };
    }
    const first = newPortfolio('My stocks', loadOldSelection());
    return { list: [first], activeId: first.id };
  } catch {
    const first = newPortfolio('My stocks');
    return { list: [first], activeId: first.id };
  }
}

export function savePortfolios(v: Portfolios) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(v));
  } catch {
    // storage unavailable (private window, blocked site data)
  }
}
