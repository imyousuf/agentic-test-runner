import { useEffect, useState } from 'react';

export type ThemeChoice = 'auto' | 'light' | 'dark';

const KEY = 'atr.theme';

function stored(): ThemeChoice {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw === 'light' || raw === 'dark' || raw === 'auto') return raw;
  } catch {
    // A blocked localStorage is not worth failing over.
  }
  return 'auto';
}

/**
 * useTheme reads the saved theme choice and writes it back to the root element.
 *
 * 'auto' leaves data-theme off, so the prefers-color-scheme query in styles.css
 * decides. The two explicit choices override it.
 */
export function useTheme(): [ThemeChoice, (next: ThemeChoice) => void] {
  const [theme, setTheme] = useState<ThemeChoice>(stored);

  useEffect(() => {
    const root = document.documentElement;
    if (theme === 'auto') root.removeAttribute('data-theme');
    else root.setAttribute('data-theme', theme);
    try {
      localStorage.setItem(KEY, theme);
    } catch {
      // Same as above: the choice just does not survive a reload.
    }
  }, [theme]);

  return [theme, setTheme];
}
