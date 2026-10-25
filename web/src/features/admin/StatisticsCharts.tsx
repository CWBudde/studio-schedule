import { Bar, BarChart, CartesianGrid, LabelList, XAxis, YAxis } from 'recharts';
import { type ChartConfig, ChartContainer, ChartTooltip } from '@/components/ui/chart';

/**
 * The one series colour of the statistics' charts, validated against the
 * card surface in both themes (≥ 3:1, inside the lightness band). It stays
 * the same whatever accent the school picks, so its contrast holds.
 */
const series = { light: 'oklch(0.44 0.14 8)', dark: 'oklch(0.66 0.14 10)' };

/** A value of a chart: its axis label, its value and the tooltip's text. */
export type ChartDatum = { key: string; label: string; value: number; text: string };

function config(label: string): ChartConfig {
  return { value: { label, theme: series } };
}

/** The tooltip of a mark: the value first, then what it belongs to. */
function DatumTooltip({
  active,
  payload,
}: {
  active?: boolean;
  payload?: { payload?: unknown }[];
}) {
  const datum = payload?.[0]?.payload as ChartDatum | undefined;
  if (!active || !datum) return null;
  return (
    <div className="rounded-md border bg-background px-2.5 py-1.5 text-xs shadow-md">
      <p className="font-semibold text-foreground">{datum.text}</p>
      <p className="text-muted-foreground">{datum.label}</p>
    </div>
  );
}

/**
 * A column chart of one series over the periods of the range: thin columns
 * rounded at the data end, a hairline grid, and a tooltip per column. The
 * figures are also in a table, which `alt` names for screen readers.
 */
export function PeriodColumnChart({
  data,
  label,
  alt,
  formatTick,
}: {
  data: ChartDatum[];
  label: string;
  alt: string;
  formatTick: (value: number) => string;
}) {
  return (
    <div role="img" aria-label={`${label}. ${alt}`}>
      <ChartContainer config={config(label)} className="aspect-auto h-60 w-full">
        <BarChart
          data={data}
          margin={{ top: 8, right: 8, left: 0, bottom: 0 }}
          accessibilityLayer={false}
        >
          <CartesianGrid vertical={false} />
          <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={12} />
          <YAxis
            tickLine={false}
            axisLine={false}
            width={56}
            allowDecimals={false}
            tickFormatter={formatTick}
          />
          <ChartTooltip cursor={{ fillOpacity: 0.4 }} content={<DatumTooltip />} />
          <Bar dataKey="value" fill="var(--color-value)" radius={[4, 4, 0, 0]} maxBarSize={24} />
        </BarChart>
      </ChartContainer>
    </div>
  );
}

/**
 * A horizontal bar chart of a share per category (e.g. utilization per
 * course type), each bar labelled at its tip.
 */
export function ShareBarChart({
  data,
  label,
  alt,
  formatTick,
}: {
  data: ChartDatum[];
  label: string;
  alt: string;
  formatTick: (value: number) => string;
}) {
  const max = Math.max(1, ...data.map((d) => d.value));
  return (
    <div role="img" aria-label={`${label}. ${alt}`}>
      <ChartContainer
        config={config(label)}
        className="aspect-auto w-full"
        style={{ height: Math.max(120, data.length * 36 + 32) }}
      >
        <BarChart
          data={data}
          layout="vertical"
          margin={{ top: 0, right: 48, left: 0, bottom: 0 }}
          accessibilityLayer={false}
        >
          <CartesianGrid horizontal={false} />
          <XAxis
            type="number"
            domain={[0, max]}
            tickLine={false}
            axisLine={false}
            tickFormatter={formatTick}
          />
          <YAxis
            type="category"
            dataKey="label"
            tickLine={false}
            axisLine={false}
            width={120}
            interval={0}
          />
          <ChartTooltip cursor={{ fillOpacity: 0.4 }} content={<DatumTooltip />} />
          <Bar dataKey="value" fill="var(--color-value)" radius={[0, 4, 4, 0]} maxBarSize={24}>
            <LabelList
              dataKey="text"
              position="right"
              offset={8}
              className="fill-foreground"
              fontSize={12}
            />
          </Bar>
        </BarChart>
      </ChartContainer>
    </div>
  );
}
