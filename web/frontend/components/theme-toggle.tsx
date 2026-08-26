"use client";

import { useEffect, useState } from "react";
import { ToggleGroup, ToggleGroupItem } from "@patternfly/react-core";
import { DesktopIcon, MoonIcon, SunIcon } from "@patternfly/react-icons";

type Theme = "light" | "dark" | "system";

const STORAGE_KEY = "authentik-bench-theme";
const DARK_CLASS = "pf-v6-theme-dark";

function systemPrefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function apply(theme: Theme) {
  const dark = theme === "dark" || (theme === "system" && systemPrefersDark());
  document.documentElement.classList.toggle(DARK_CLASS, dark);
}

/**
 * ThemeToggle switches PatternFly's dark theme class on <html>. Defaults to
 * "system" (no stored preference yet), and while on "system" keeps tracking
 * prefers-color-scheme changes live rather than freezing at whatever it was
 * on page load.
 */
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>("system");

  useEffect(() => {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    const initial: Theme = stored === "light" || stored === "dark" ? stored : "system";
    setTheme(initial);
    apply(initial);

    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onSystemChange = () => {
      if ((window.localStorage.getItem(STORAGE_KEY) ?? "system") === "system") {
        apply("system");
      }
    };
    media.addEventListener("change", onSystemChange);
    return () => media.removeEventListener("change", onSystemChange);
  }, []);

  function choose(next: Theme) {
    setTheme(next);
    if (next === "system") {
      window.localStorage.removeItem(STORAGE_KEY);
    } else {
      window.localStorage.setItem(STORAGE_KEY, next);
    }
    apply(next);
  }

  return (
    <ToggleGroup aria-label="Colour theme">
      <ToggleGroupItem
        icon={<SunIcon />}
        aria-label="Light"
        buttonId="theme-light"
        isSelected={theme === "light"}
        onChange={() => choose("light")}
      />
      <ToggleGroupItem
        icon={<MoonIcon />}
        aria-label="Dark"
        buttonId="theme-dark"
        isSelected={theme === "dark"}
        onChange={() => choose("dark")}
      />
      <ToggleGroupItem
        icon={<DesktopIcon />}
        aria-label="System"
        buttonId="theme-system"
        isSelected={theme === "system"}
        onChange={() => choose("system")}
      />
    </ToggleGroup>
  );
}
