import { useEffect, useState } from 'react';

/** isDarkTheme reads html[data-theme] - main.tsx's one source of truth for
 * the active theme, which @goauthentik/theme itself keys off. */
export function isDarkTheme(): boolean {
  return document.documentElement.dataset.theme === 'dark';
}

/** useDarkTheme re-renders whenever main.tsx flips html[data-theme], for
 * anything drawn outside CSS (canvas charts, Monaco, inline SVG colors). */
export function useDarkTheme(): boolean {
  const [dark, setDark] = useState(isDarkTheme);
  useEffect(() => {
    const observer = new MutationObserver(() => setDark(isDarkTheme()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    return () => observer.disconnect();
  }, []);
  return dark;
}
