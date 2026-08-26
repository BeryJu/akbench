import type { Metadata } from "next";
import "@patternfly/react-core/dist/styles/base.css";
import { AppShell } from "@/components/app-shell";

export const metadata: Metadata = {
  title: "authentik-bench",
  description: "Kubernetes-native benchmark harness for authentik",
};

// Sets the dark-theme class before first paint, so there is no flash of the
// wrong theme while React hydrates. Keep the storage key and class name in
// sync with components/theme-toggle.tsx - this has to be a plain inline
// script (it runs before any bundle loads), so it can't import the constants.
const themeScript = `(function () {
  var stored = window.localStorage.getItem("authentik-bench-theme");
  var dark = stored === "dark" || (stored !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("pf-v6-theme-dark", dark);
})();`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body>
        <AppShell>{children}</AppShell>
      </body>
    </html>
  );
}
