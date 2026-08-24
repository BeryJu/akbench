import type { Metadata } from "next";
import { Nav } from "@/components/nav";
import "./globals.css";

export const metadata: Metadata = {
  title: "authentik-bench",
  description: "Kubernetes-native benchmark harness for authentik",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <div className="shell">
          <aside className="sidebar">
            <a className="brand" href="/">
              authentik<span>-bench</span>
            </a>
            <Nav />
          </aside>
          <main>{children}</main>
        </div>
      </body>
    </html>
  );
}
