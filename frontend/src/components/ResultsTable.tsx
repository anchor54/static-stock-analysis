import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode, type Ref } from 'react';
import type { Column, SortState, ViewModes } from '../columns';
import { copyText } from '../copyTable';
import { cx } from '../format';
import type { Row } from '../types';

interface Props {
  tableRef?: Ref<HTMLTableElement>;
  /** Visible columns; the first is pinned and holds the row's remove button. */
  columns: Column[];
  rows: Row[];
  /** Selected entries with no result row; `reason` is set when the last scan skipped them. */
  pending: { entry: string; reason?: string }[];
  sort: SortState;
  modes: ViewModes;
  onSort(id: string): void;
  /** Content widths in px set by dragging a header's edge; other columns fit their content. */
  widths: Record<string, number>;
  /** Sets a column's width, or resets it to fit its content with null. */
  onResize(id: string, width: number | null): void;
  /** Hides columns, one or a whole group; never the pinned first column. */
  onHide(ids: string[]): void;
  /** Removes an entry (a row's query) from the selection. */
  onRemove(entry: string): void;
}

/**
 * Consecutive columns sharing a group, for the grouped header row. The first column is pinned while
 * scrolling sideways, so it gets a group cell of its own.
 */
function groupsOf(columns: Column[]) {
  return columns.reduce<{ name: string; span: number; start: number }[]>((acc, c, i) => {
    const last = acc[acc.length - 1];
    if (last && last.name === c.group && i > 1) last.span++;
    else acc.push({ name: c.group, span: 1, start: i });
    return acc;
  }, []);
}

/** The pinned first column's classes, on top of its own. */
const pin = (i: number) => i === 0 && 'pin';

export const MIN_WIDTH = 32;
export const MAX_WIDTH = 600;
const clampWidth = (w: number) => Math.round(Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, w)));

/** Holds a resized column's cell content at its width, cutting off what doesn't fit with "…". */
function Fit({ width, children }: { width?: number; children: ReactNode }) {
  return width ? (
    <div className="fit" style={{ width }}>
      {children}
    </div>
  ) : (
    children
  );
}

/** Drag handle on a header's right edge; ← and → also resize, double-click resets. */
function ResizeHandle({ label, width, onResize }: { label: string; width?: number; onResize(width: number | null): void }) {
  const drag = useRef<{ x: number; start: number } | null>(null);

  /** The column's current content width, from its header cell. */
  function measure(handle: HTMLElement) {
    const th = handle.parentElement!;
    const cs = getComputedStyle(th);
    return th.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
  }

  return (
    <span
      className="col-resize"
      role="separator"
      aria-orientation="vertical"
      aria-label={`Resize ${label} column`}
      aria-valuenow={width}
      tabIndex={0}
      title="Drag to resize · double-click to fit"
      onPointerDown={(e: PointerEvent<HTMLSpanElement>) => {
        e.preventDefault(); // no text selection
        e.currentTarget.setPointerCapture(e.pointerId);
        drag.current = { x: e.clientX, start: measure(e.currentTarget) };
      }}
      onPointerMove={(e) => {
        if (drag.current) onResize(clampWidth(drag.current.start + e.clientX - drag.current.x));
      }}
      onPointerUp={() => (drag.current = null)}
      onPointerCancel={() => (drag.current = null)}
      onClick={(e) => e.stopPropagation() /* don't sort */}
      onDoubleClick={(e) => {
        e.stopPropagation();
        onResize(null);
      }}
      onKeyDown={(e: KeyboardEvent<HTMLSpanElement>) => {
        if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
        e.preventDefault();
        const start = width ?? measure(e.currentTarget);
        onResize(clampWidth(start + (e.key === 'ArrowRight' ? 10 : -10)));
      }}
    />
  );
}

/** Copies one stock's symbol; shows a tick for a moment once copied. */
function CopyButton({ text }: { text: string }) {
  const [state, setState] = useState<'idle' | 'done' | 'failed'>('idle');
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const label = state === 'done' ? `Copied ${text}` : state === 'failed' ? `Couldn't copy ${text}` : `Copy ${text}`;
  return (
    <button
      type="button"
      className={cx('row-copy', state === 'done' && 'done')}
      aria-label={label}
      title={label}
      onClick={() => {
        copyText(text).then(
          () => setState('done'),
          () => setState('failed'),
        );
        clearTimeout(timer.current);
        timer.current = setTimeout(() => setState('idle'), 1500);
      }}
    >
      <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
        {state === 'done' ? (
          <path d="M3 8.5l3 3 7-7" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
        ) : (
          <path
            d="M5.5 5.5V3.5a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1h-2M3.5 5.5h6a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1h-6a1 1 0 0 1-1-1v-6a1 1 0 0 1 1-1z"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.4"
            strokeLinejoin="round"
          />
        )}
      </svg>
    </button>
  );
}

function RemoveButton({ label, onClick }: { label: string; onClick(): void }) {
  return (
    <button type="button" className="row-del" aria-label={`Remove ${label}`} title={`Remove ${label}`} onClick={onClick}>
      <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
        <path
          d="M6 2h4M2.5 4h11M4 4l.7 9.1a1 1 0 0 0 1 .9h4.6a1 1 0 0 0 1-.9L12 4M6.5 6.5v5M9.5 6.5v5"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </button>
  );
}

/** `label` is the full action, e.g. "Hide SMA columns". */
function HideButton({ label, onClick }: { label: string; onClick(): void }) {
  return (
    <button
      type="button"
      className="col-hide"
      aria-label={label}
      title={label}
      onClick={(e) => {
        e.stopPropagation(); // don't sort
        onClick();
      }}
    >
      <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
        <path
          d="M2 2l12 12M6.6 3.6A6.6 6.6 0 0 1 8 3.5c3.5 0 6 4.5 6 4.5a11 11 0 0 1-1.9 2.4M10 12a5.6 5.6 0 0 1-2 .5C4.5 12.5 2 8 2 8a11 11 0 0 1 2.4-2.9M6.6 6.6a2 2 0 0 0 2.8 2.8"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </button>
  );
}

export function ResultsTable({ tableRef, columns, rows, pending, sort, modes, onSort, widths, onResize, onHide, onRemove }: Props) {
  const groups = groupsOf(columns);
  const startsGroup = (i: number) => i > 0 && columns[i - 1].group !== columns[i].group;
  return (
    <div className="table-wrap">
      <table ref={tableRef}>
        <thead>
          <tr className="groups">
            {groups.map((g, i) => {
              const ids = columns.slice(g.start, g.start + g.span).map((c) => c.id);
              // The rest of the pinned column's group sits under it, unlabelled.
              const rest = i === 1 && g.name === groups[0].name;
              // Once a column under it is resized, the label no longer sets the width, so it can't hold a
              // narrowed column open; it's cut off with "…" instead.
              const clip = ids.some((id) => widths[id]);
              return (
                <th key={g.start} colSpan={g.span} className={cx(pin(g.start), startsGroup(g.start) && 'grp-start')}>
                  <span className={cx('grp-in', clip && 'clip')}>
                    <span className="grp-label">{rest ? '' : g.name}</span>
                    {i > 0 && (
                      <HideButton
                        label={rest ? `Hide ${columns[g.start].label} column` : `Hide ${g.name} ${ids.length > 1 ? 'columns' : 'column'}`}
                        onClick={() => onHide(ids)}
                      />
                    )}
                  </span>
                </th>
              );
            })}
          </tr>
          <tr className="cols">
            {columns.map((c, i) => {
              const sorted = sort.id === c.id;
              const name = c.group === c.label ? c.label : `${c.group} ${c.label}`;
              return (
                <th
                  key={c.id}
                  scope="col"
                  tabIndex={0}
                  className={cx(c.align === 'left' && 'left', pin(i), startsGroup(i) && 'grp-start')}
                  aria-sort={sorted ? (sort.dir > 0 ? 'ascending' : 'descending') : 'none'}
                  title={`Sort by ${name}`}
                  onClick={() => onSort(c.id)}
                  onKeyDown={(e) => {
                    if (e.target !== e.currentTarget) return; // a key on the hide button
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      onSort(c.id);
                    }
                  }}
                >
                  <div className="th-in" style={widths[c.id] ? { width: widths[c.id] } : undefined}>
                    <span className="th-label">{c.label}</span>
                    <span className="arrow">{sorted ? (sort.dir > 0 ? '↑' : '↓') : ''}</span>
                    {i > 0 && <HideButton label={`Hide ${name} column`} onClick={() => onHide([c.id])} />}
                  </div>
                  <ResizeHandle label={name} width={widths[c.id]} onResize={(w) => onResize(c.id, w)} />
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.ticker}>
              {columns.map((c, i) => {
                const cell = c.cell(r, modes);
                return (
                  <td
                    key={c.id}
                    title={cell.title}
                    className={cx(c.align === 'left' && 'left', pin(i), c.className, cell.className, startsGroup(i) && 'grp-start')}
                  >
                    {i === 0 && (
                      <>
                        <CopyButton text={r.symbol} />
                        <RemoveButton label={r.symbol} onClick={() => onRemove(r.query)} />
                      </>
                    )}
                    <Fit width={widths[c.id]}>{cell.content}</Fit>
                  </td>
                );
              })}
            </tr>
          ))}
          {pending.map((p) => (
            <tr key={`pending:${p.entry.toUpperCase()}`} className={cx('pending', p.reason && 'bad')}>
              <td className="left pin sym">
                <CopyButton text={p.entry} />
                <RemoveButton label={p.entry} onClick={() => onRemove(p.entry)} />
                <Fit width={widths[columns[0].id]}>{p.entry}</Fit>
              </td>
              <td className="left note" colSpan={columns.length - 1}>
                {p.reason ? `Skipped: ${p.reason}` : 'Not analysed yet. Press Analyze.'}
              </td>
            </tr>
          ))}
          {!rows.length && !pending.length && (
            <tr>
              <td className="empty" colSpan={columns.length}>
                Search for stocks above and press Analyze.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
