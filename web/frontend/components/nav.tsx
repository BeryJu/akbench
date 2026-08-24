"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  { href: "/", label: "Home" },
  { href: "/clusters", label: "Clusters" },
  { href: "/topologies", label: "Topologies" },
  { href: "/testcases", label: "Test cases" },
  { href: "/testruns", label: "Test runs" },
  { href: "/compare", label: "Compare" },
];

export function Nav() {
  const pathname = usePathname();
  return (
    <nav className="nav">
      {links.map((link) => {
        // "/" would otherwise be marked current on every page.
        const current = link.href === "/" ? pathname === "/" : pathname.startsWith(link.href);
        return (
          <Link key={link.href} href={link.href} aria-current={current ? "page" : undefined}>
            {link.label}
          </Link>
        );
      })}
    </nav>
  );
}
