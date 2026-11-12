import { useEffect, useState } from 'react';

type Theme = 'system' | 'light' | 'dark';

// Read before the page draws by the inline script in index.html; keep the key in step with it.
const THEME_KEY = 'stockScanner.theme';

function loadTheme(): Theme {
  try {
    const t = localStorage.getItem(THEME_KEY);
    return t === 'light' || t === 'dark' ? t : 'system';
  } catch {
    return 'system';
  }
}

/** Sets <html data-theme>, which styles.css uses to override the Windows setting. */
function applyTheme(t: Theme) {
  const root = document.documentElement;
  if (t === 'system') delete root.dataset.theme;
  else root.dataset.theme = t;
}

const OPTIONS: [Theme, string, string][] = [
  ['system', 'Auto', 'Follow the Windows light/dark setting'],
  ['light', 'Light', 'Always light'],
  ['dark', 'Dark', 'Always dark'],
];

/** Auto / Light / Dark switch, remembered in this browser. */
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(loadTheme);

  useEffect(() => {
    applyTheme(theme);
    try {
      if (theme === 'system') localStorage.removeItem(THEME_KEY);
      else localStorage.setItem(THEME_KEY, theme);
    } catch {
      // storage unavailable
    }
  }, [theme]);

  return (
    <div className="theme" role="group" aria-label="Theme">
      {OPTIONS.map(([value, label, title]) => (
        <button key={value} type="button" aria-pressed={theme === value} title={title} onClick={() => setTheme(value)}>
          {label}
        </button>
      ))}
    </div>
  );
}
