// Copies the results table to the clipboard as styled HTML, which Excel (and Google Sheets) keep when pasting.

/**
 * Inline styles per class. Excel ignores stylesheets and CSS variables, so these are the light-theme
 * values from styles.css, used even when the page is in dark mode.
 */
const CLASS_STYLES: Record<string, string> = {
  up: 'color:#127a3a',
  down: 'color:#c0282d',
  missing: 'color:#6b6b70',
  sym: 'font-weight:bold',
  name: 'color:#6b6b70',
  pos: 'background:#e3f4e8;color:#127a3a;font-weight:bold',
  neg: 'background:#fbe5e5;color:#c0282d;font-weight:bold',
  hot: 'background:#fdecc8;color:#8a5a00',
  strong: 'background:#dff1e3;color:#17663a',
  weak: 'background:#fbe3e1;color:#a3262a',
  cold: 'background:#dde8fb;color:#1f4c9c',
  s1: 'background:#eee4cf;color:#6b5426;font-weight:bold',
  s2: 'background:#d8f0de;color:#13612f;font-weight:bold',
  s3: 'background:#fde2c7;color:#8c4a06;font-weight:bold',
  s4: 'background:#f9d9d9;color:#9a1f24;font-weight:bold',
};

const BORDER = 'border:1px solid #dcdcd8';
const HEAD = `${BORDER};background:#efefeb;font-weight:bold`;
/**
 * Marks a cell as text. Without it Excel turns symbols into numbers or dates, and a cell starting with
 * "+" (like "+0.140 ▲") makes it read the next cell as text too.
 */
const AS_TEXT = "mso-number-format:'\\@'";

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/** A cell's text without its buttons, sort arrow and note icon. */
function cellText(el: Element): string {
  const copy = el.cloneNode(true) as Element;
  copy.querySelectorAll('button, .arrow, .warn').forEach((n) => n.remove());
  return (copy.textContent ?? '').replace(/\s+/g, ' ').trim();
}

/**
 * A plain number or percentage as a raw value plus an Excel number format, so it pastes as a number
 * whatever Excel's locale (the page groups digits the Indian way, e.g. 1,30,000.00).
 */
function asNumber(text: string): { value: string; format: string } | null {
  const m = /^([+-]?)([\d,]+)(?:\.(\d+))?(%?)$/.exec(text);
  if (!m) return null;
  const [, sign, int, dec = '', pct] = m;
  const digits = int.replace(/,/g, '');
  const base = (pct ? '0' : '#,##0') + (dec ? `.${'0'.repeat(dec.length)}` : '') + (pct ? '%' : '');
  const n = `${sign === '-' ? '-' : ''}${digits}${dec ? `.${dec}` : ''}`;
  const value = pct ? String(Number((Number(n) / 100).toFixed(dec.length + 2))) : n; // toFixed drops float noise like 0.0113999…
  return { value, format: sign ? `+${base};-${base};${base}` : base };
}

/** Styles from the cell's classes and its inner chip, RSI or stage badge (Excel only colours whole cells). */
function cellStyle(el: Element): string {
  const classes = [el, ...el.querySelectorAll('span')].flatMap((n) => [...n.classList]);
  const align = el.classList.contains('left') ? 'left' : 'right';
  return [BORDER, `text-align:${align}`, ...classes.map((c) => CLASS_STYLES[c])].filter(Boolean).join(';');
}

/** Builds the HTML and tab-separated text for the table's analysed rows. */
export function tableForExcel(table: HTMLTableElement, title: string): { html: string; text: string } {
  const out: string[] = [];
  const tsv: string[] = [title];
  const groups = [...table.querySelectorAll('thead tr.groups th')];
  const cols = [...table.querySelectorAll('thead tr.cols th')];
  const width = cols.length;

  out.push(`<tr><td colspan="${width}" style="font-weight:bold;font-size:13pt">${esc(title)}</td></tr>`);
  out.push(
    `<tr>${groups
      .map((th) => `<th colspan="${(th as HTMLTableCellElement).colSpan}" style="${HEAD};text-align:center;color:#6b6b70">${esc(cellText(th).toUpperCase())}</th>`)
      .join('')}</tr>`,
  );
  out.push(
    `<tr>${cols.map((th) => `<th style="${HEAD};${AS_TEXT};text-align:${th.classList.contains('left') ? 'left' : 'right'}">${esc(cellText(th))}</th>`).join('')}</tr>`,
  );
  tsv.push(cols.map(cellText).join('\t'));

  // Rows still waiting for a scan, or skipped by it, have no data to copy.
  for (const tr of table.querySelectorAll('tbody tr:not(.pending)')) {
    const cells = [...tr.querySelectorAll('td')];
    if (cells.length !== width) continue; // the empty-table message
    out.push(
      `<tr>${cells
        .map((td) => {
          const text = cellText(td);
          const n = td.classList.contains('left') ? null : asNumber(text);
          return n
            ? `<td style="${cellStyle(td)};mso-number-format:'${n.format}'">${n.value}</td>`
            : `<td style="${cellStyle(td)};${AS_TEXT}">${esc(text)}</td>`;
        })
        .join('')}</tr>`,
    );
    tsv.push(cells.map(cellText).join('\t'));
  }

  const html = `<html><head><meta charset="utf-8"></head><body><table style="border-collapse:collapse;font-family:Calibri,Arial,sans-serif;font-size:11pt">${out.join('')}</table></body></html>`;
  return { html, text: tsv.join('\n') };
}

/** Puts the table on the clipboard as HTML, with plain text for apps that can't take HTML. */
export async function copyForExcel(table: HTMLTableElement, title: string): Promise<void> {
  const { html, text } = tableForExcel(table, title);
  if (navigator.clipboard && typeof ClipboardItem !== 'undefined') {
    await navigator.clipboard.write([
      new ClipboardItem({ 'text/html': new Blob([html], { type: 'text/html' }), 'text/plain': new Blob([text], { type: 'text/plain' }) }),
    ]);
    return;
  }
  // The Clipboard API needs a secure page (it is on 127.0.0.1, not on a LAN address): copy a selection instead.
  const box = document.createElement('div');
  box.style.cssText = 'position:fixed;left:-9999px;top:0';
  box.innerHTML = html;
  document.body.append(box);
  const range = document.createRange();
  range.selectNodeContents(box);
  const sel = getSelection();
  sel?.removeAllRanges();
  sel?.addRange(range);
  const ok = document.execCommand('copy');
  sel?.removeAllRanges();
  box.remove();
  if (!ok) throw new Error('The browser blocked copying to the clipboard.');
}
