import t100 from "@patternfly/react-tokens/dist/esm/chart_theme_multi_color_ordered_ColorScale_100";
import t200 from "@patternfly/react-tokens/dist/esm/chart_theme_multi_color_ordered_ColorScale_200";
import t300 from "@patternfly/react-tokens/dist/esm/chart_theme_multi_color_ordered_ColorScale_300";
import t400 from "@patternfly/react-tokens/dist/esm/chart_theme_multi_color_ordered_ColorScale_400";
import t500 from "@patternfly/react-tokens/dist/esm/chart_theme_multi_color_ordered_ColorScale_500";
import t600 from "@patternfly/react-tokens/dist/esm/chart_theme_multi_color_ordered_ColorScale_600";
import t700 from "@patternfly/react-tokens/dist/esm/chart_theme_multi_color_ordered_ColorScale_700";
import t800 from "@patternfly/react-tokens/dist/esm/chart_theme_multi_color_ordered_ColorScale_800";

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

/** PatternFly's own qualitative chart palette, cycled by index so the same
 * test keeps the same colour across every chart on a page - not a hand-
 * picked palette of this project's own. */
const palette = [t100, t200, t300, t400, t500, t600, t700, t800].map((token) => token.var);

export function colorFor(index: number): string {
  return palette[index % palette.length];
}
