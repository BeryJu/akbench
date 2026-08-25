/** formatMetricValue renders a CompareMetric value the same way in the compare
 * table and every chart tooltip/axis, so a number means the same thing
 * wherever it appears. */
export function formatMetricValue(value: number | undefined, unit: string): string {
  if (value === undefined) {
    return "-";
  }
  if (unit === "ratio") {
    return `${(value * 100).toFixed(1)}%`;
  }
  if (unit === "s") {
    return value < 1 ? `${(value * 1000).toFixed(0)} ms` : `${value.toFixed(2)} s`;
  }
  return value >= 100 ? value.toFixed(0) : value.toFixed(2);
}

/** A qualitative palette for per-test series, cycled by index so the same
 * test keeps the same colour across every chart on a page. */
const palette = [
  "#fd4b2d",
  "#2f7de1",
  "#1a7f4b",
  "#b06000",
  "#8654d6",
  "#c02a20",
  "#1596a9",
  "#c73e8f",
];

export function colorFor(index: number): string {
  return palette[index % palette.length];
}
