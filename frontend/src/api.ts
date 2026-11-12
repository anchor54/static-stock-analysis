import type { ScanResult, SymbolList } from './types';

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  let resp: Response;
  try {
    resp = await fetch(url, init);
  } catch {
    throw new Error('Could not reach the scanner server. Is it running?');
  }
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error(body.error || `Request failed (HTTP ${resp.status}).`);
  }
  return body as T;
}

export function analyze(symbols: string[]): Promise<ScanResult> {
  return request<ScanResult>('/api/analyze', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ symbols }),
  });
}

export function fetchSymbols(): Promise<SymbolList> {
  return request<SymbolList>('/api/symbols');
}
