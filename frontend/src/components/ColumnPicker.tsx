import { useEffect, useId, useRef, useState } from 'react';
import { COLUMNS, type Column } from '../columns';

interface Props {
  /** Ids of hidden columns. */
  hidden: Set<string>;
  onChange(next: Set<string>): void;
  /** Whether any column has been resized. */
  resized: boolean;
  onResetWidths(): void;
}

/** Columns grouped as in the table header, for the checklist. */
const GROUPS = COLUMNS.reduce<{ name: string; columns: Column[] }[]>((acc, c) => {
  const last = acc[acc.length - 1];
  if (last && last.name === c.group) last.columns.push(c);
  else acc.push({ name: c.group, columns: [c] });
  return acc;
}, []);

/** The pinned first column holds the row's remove button, so it can't be hidden. */
const LOCKED = COLUMNS[0].id;

/** Toolbar button with a checklist of the table's columns. */
export function ColumnPicker({ hidden, onChange, resized, onResetWidths }: Props) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // Close on a click outside or on Escape.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  function toggle(ids: string[], show: boolean) {
    const next = new Set(hidden);
    for (const c of ids) {
      if (c === LOCKED) continue;
      if (show) next.delete(c);
      else next.add(c);
    }
    onChange(next);
  }

  return (
    <div className="col-picker" ref={ref}>
      <button type="button" aria-expanded={open} aria-controls={`${id}-panel`} onClick={() => setOpen((o) => !o)}>
        Columns{hidden.size > 0 && ` (${hidden.size} hidden)`} ▾
      </button>
      {open && (
        <div className="col-panel" id={`${id}-panel`} role="group" aria-label="Visible columns">
          {GROUPS.map((g) => {
            const ids = g.columns.map((c) => c.id).filter((c) => c !== LOCKED);
            const shown = ids.filter((c) => !hidden.has(c)).length;
            return (
              <fieldset key={g.name}>
                {/* Group heading doubles as a show/hide-all for the group when it has several columns. */}
                <legend>
                  {ids.length > 1 ? (
                    <label>
                      <input
                        type="checkbox"
                        checked={shown === ids.length}
                        ref={(el) => {
                          if (el) el.indeterminate = shown > 0 && shown < ids.length;
                        }}
                        onChange={(e) => toggle(ids, e.target.checked)}
                      />
                      {g.name}
                    </label>
                  ) : (
                    g.name
                  )}
                </legend>
                {g.columns.map((c) => (
                  <label key={c.id} className={c.id === LOCKED ? 'locked' : undefined} title={c.id === LOCKED ? 'Always shown' : undefined}>
                    <input
                      type="checkbox"
                      checked={!hidden.has(c.id)}
                      disabled={c.id === LOCKED}
                      onChange={(e) => toggle([c.id], e.target.checked)}
                    />
                    {c.label}
                  </label>
                ))}
              </fieldset>
            );
          })}
          <div className="col-actions">
            <button type="button" className="link" disabled={!hidden.size} onClick={() => onChange(new Set())}>
              Show all
            </button>
            <button type="button" className="link" disabled={!resized} onClick={onResetWidths} title="Fit every column to its content">
              Reset widths
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
