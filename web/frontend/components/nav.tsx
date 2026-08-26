"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Nav as PFNav, NavItem, NavList } from "@patternfly/react-core";

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
    <PFNav aria-label="Global navigation">
      <NavList>
        {links.map((link) => {
          // "/" would otherwise be marked current on every page.
          const current = link.href === "/" ? pathname === "/" : pathname.startsWith(link.href);
          return (
            <NavItem key={link.href} isActive={current}>
              <Link href={link.href}>{link.label}</Link>
            </NavItem>
          );
        })}
      </NavList>
    </PFNav>
  );
}
