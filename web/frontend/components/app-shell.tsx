"use client";

import Link from "next/link";
import {
  Flex,
  Masthead,
  MastheadBrand,
  MastheadContent,
  MastheadMain,
  Page,
  PageSection,
  PageSidebar,
  PageSidebarBody,
} from "@patternfly/react-core";
import { Nav } from "@/components/nav";
import { ThemeToggle } from "@/components/theme-toggle";

export function AppShell({ children }: { children: React.ReactNode }) {
  return (
    <Page
      isManagedSidebar
      masthead={
        <Masthead>
          <MastheadMain>
            <MastheadBrand>
              <Link href="/" style={{ fontWeight: 700, fontSize: "1.1rem" }}>
                authentik
                <span style={{ color: "var(--pf-t--global--text--color--brand--default)" }}>
                  -bench
                </span>
              </Link>
            </MastheadBrand>
          </MastheadMain>
          <MastheadContent>
            <Flex justifyContent={{ default: "justifyContentFlexEnd" }}>
              <ThemeToggle />
            </Flex>
          </MastheadContent>
        </Masthead>
      }
      sidebar={
        <PageSidebar>
          <PageSidebarBody>
            <Nav />
          </PageSidebarBody>
        </PageSidebar>
      }
    >
      <PageSection>{children}</PageSection>
    </Page>
  );
}
