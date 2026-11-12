import type { Mode } from '../columns';

interface Props {
  label: string;
  value: Mode;
  options: [Mode, string][];
  onChange(mode: Mode): void;
}

/** Two-way toggle between distance and level display. */
export function Segmented({ label, value, options, onChange }: Props) {
  return (
    <div className="seg" role="group" aria-label={`${label} columns`}>
      <span className="lbl">{label}</span>
      {options.map(([mode, text]) => (
        <button key={mode} type="button" aria-pressed={value === mode} onClick={() => onChange(mode)}>
          {text}
        </button>
      ))}
    </div>
  );
}
