"use client";

import { useState } from "react";
import { Dropdown, DropdownItem, DropdownList, MenuToggle } from "@patternfly/react-core";
import { OutlinedClockIcon } from "@patternfly/react-icons";

// RELATIVE_RANGES mirrors the classic Grafana quick-range list (Last 5m
// through Last 30d) - the same relative-time vocabulary every dashboard in
// the reference Ansible harness this project replaces used, so a value here
// means the same thing it always did.
export const RELATIVE_RANGES: { label: string; seconds: number }[] = [
  { label: "Last 5 minutes", seconds: 5 * 60 },
  { label: "Last 15 minutes", seconds: 15 * 60 },
  { label: "Last 30 minutes", seconds: 30 * 60 },
  { label: "Last 1 hour", seconds: 60 * 60 },
  { label: "Last 3 hours", seconds: 3 * 60 * 60 },
  { label: "Last 6 hours", seconds: 6 * 60 * 60 },
  { label: "Last 12 hours", seconds: 12 * 60 * 60 },
  { label: "Last 24 hours", seconds: 24 * 60 * 60 },
  { label: "Last 2 days", seconds: 2 * 24 * 60 * 60 },
  { label: "Last 7 days", seconds: 7 * 24 * 60 * 60 },
  { label: "Last 30 days", seconds: 30 * 24 * 60 * 60 },
];

export const DEFAULT_RANGE = RELATIVE_RANGES[3]; // Last 1 hour

/** TimeRangePicker is the Grafana-style relative-range control every chart-
 * bearing page (Home, Compare, a TestCase's own page) shares, so "last 1h"
 * means the same window everywhere it appears. */
export function TimeRangePicker({
  seconds,
  onChange,
}: {
  seconds: number;
  onChange: (seconds: number) => void;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const current = RELATIVE_RANGES.find((r) => r.seconds === seconds) ?? DEFAULT_RANGE;

  return (
    <Dropdown
      isOpen={isOpen}
      onSelect={() => setIsOpen(false)}
      onOpenChange={setIsOpen}
      toggle={(toggleRef) => (
        <MenuToggle
          ref={toggleRef}
          icon={<OutlinedClockIcon />}
          onClick={() => setIsOpen((open) => !open)}
          isExpanded={isOpen}
        >
          {current.label}
        </MenuToggle>
      )}
    >
      <DropdownList>
        {RELATIVE_RANGES.map((range) => (
          <DropdownItem key={range.label} value={range.seconds} onClick={() => onChange(range.seconds)}>
            {range.label}
          </DropdownItem>
        ))}
      </DropdownList>
    </Dropdown>
  );
}
