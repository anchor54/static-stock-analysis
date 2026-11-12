import { useRef, useState } from 'react';
import { cx } from '../format';
import type { Portfolio } from '../portfolios';

interface Props {
  list: Portfolio[];
  activeId: string;
  onSwitch(id: string): void;
  onCreate(name: string): void;
  onRename(id: string, name: string): void;
  onDelete(id: string): void;
}

/** What's being named: a portfolio being renamed, or 'new' for one being created. */
type Editing = { id: string; value: string } | null;

function NameInput({ editing, onChange, onDone }: { editing: NonNullable<Editing>; onChange(v: string): void; onDone(save: boolean): void }) {
  return (
    <input
      className="p-name"
      autoFocus
      aria-label={editing.id === 'new' ? 'New portfolio name' : 'Portfolio name'}
      placeholder="Portfolio name"
      value={editing.value}
      maxLength={40}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') onDone(true);
        else if (e.key === 'Escape') onDone(false);
      }}
      onBlur={() => onDone(true)}
    />
  );
}

/** Row of portfolio tabs; the active one can be renamed or deleted. */
export function PortfolioTabs({ list, activeId, onSwitch, onCreate, onRename, onDelete }: Props) {
  const [editing, setEditingState] = useState<Editing>(null);
  // Enter saves, then the input's blur would save again before the re-render; this stops the second one.
  const open = useRef(false);
  const setEditing = (e: Editing) => {
    open.current = !!e;
    setEditingState(e);
  };

  function done(save: boolean) {
    if (!editing || !open.current) return;
    open.current = false;
    const name = editing.value.trim();
    if (save && name) {
      if (editing.id === 'new') onCreate(name);
      else onRename(editing.id, name);
    }
    setEditing(null);
  }

  function remove(p: Portfolio) {
    const what = p.stocks.length === 1 ? '1 stock' : `${p.stocks.length} stocks`;
    if (window.confirm(`Delete portfolio “${p.name}” (${what})? This can't be undone.`)) onDelete(p.id);
  }

  const setValue = (value: string) => setEditingState((e) => e && { ...e, value });

  return (
    <div className="portfolios" role="group" aria-label="Portfolios">
      {list.map((p) => {
        const active = p.id === activeId;
        if (editing?.id === p.id) return <NameInput key={p.id} editing={editing} onChange={setValue} onDone={done} />;
        return (
          <div key={p.id} className={cx('p-tab', active && 'active')}>
            <button
              type="button"
              className="p-switch"
              aria-pressed={active}
              title={active ? 'Double-click to rename' : `Show ${p.name}`}
              onClick={() => onSwitch(p.id)}
              onDoubleClick={() => setEditing({ id: p.id, value: p.name })}
            >
              {p.name}
              <span className="p-count">{p.stocks.length}</span>
            </button>
            {active && (
              <>
                <button type="button" className="p-icon" aria-label={`Rename ${p.name}`} title="Rename" onClick={() => setEditing({ id: p.id, value: p.name })}>
                  <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                    <path d="M10.5 2.5l3 3L6 13H3v-3z" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" />
                  </svg>
                </button>
                {list.length > 1 && (
                  <button type="button" className="p-icon" aria-label={`Delete ${p.name}`} title="Delete portfolio" onClick={() => remove(p)}>
                    <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
                      <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                    </svg>
                  </button>
                )}
              </>
            )}
          </div>
        );
      })}
      {editing?.id === 'new' ? (
        <NameInput editing={editing} onChange={setValue} onDone={done} />
      ) : (
        <button type="button" className="p-new" onClick={() => setEditing({ id: 'new', value: '' })}>
          + New portfolio
        </button>
      )}
    </div>
  );
}
