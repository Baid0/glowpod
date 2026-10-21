import { IconArrowDown, IconArrowUp, IconMinus } from "@tabler/icons-react";

import { useTween } from "../../lib/motion";
import { Badge, type StatusTone } from "../Badge";
import { Button } from "../Button";
import {
  arcPath,
  DEFAULT_THRESHOLDS,
  fractionOf,
  layoutFor,
  pointAt,
  tickLine,
  ZONE_WORDS,
  zoneFor,
  type GaugeSize,
  type GaugeZone,
  type Thresholds,
} from "./geometry";
import styles from "./HalfGauge.module.css";

export type GaugeState = "loading" | "live" | "stale" | "no-metrics" | "error";

export interface HalfGaugeProps {
  label: string;
  value: number;
  max: number;
  /** Unit of value and max, e.g. "cores", "GiB", "pods". */
  unit?: string;
  /** Share reserved by pod requests, in the same unit; drawn as a thin inner arc. */
  requested?: number;
  thresholds?: Thresholds;
  size?: GaugeSize;
  /** Change over the last hour as a fraction of max (0.04 = +4 points). */
  trend?: number;
  /** Replaces the trend text, e.g. "12 in 1h" for pod counts. */
  trendLabel?: string;
  state?: GaugeState;
  /** percent shows "62%"; value shows the raw number (e.g. network MB/s). */
  display?: "percent" | "value";
  /** Replaces the "24.8 of 40 cores" line under the value. */
  detail?: string;
  /** Footer text on the left when there is no requested share, e.g. "18 volumes". */
  note?: string;
  /** Age of the data, e.g. "updated 3 min ago". Shown when stale. */
  updated?: string;
  /** Header with name and status badge. Defaults to on for size L. */
  showHeader?: boolean;
  /** Size S only: put the value inside the arc instead of beside it (mini gauges). */
  valueInside?: boolean;
  onRetry?: () => void;
  /** Link shown in the no-metrics state. */
  helpHref?: string;
  helpLabel?: string;
}

const FIRST_SWEEP_MS = 600;
const UPDATE_MS = 300;
const METRICS_SERVER_DOCS = "https://github.com/kubernetes-sigs/metrics-server#installation";

const numberFormat = new Intl.NumberFormat("en", { maximumFractionDigits: 1 });
const formatNumber = (n: number) => numberFormat.format(n);
/** The big center number: one decimal below 10 ("4.2"), whole numbers above ("120"). */
const formatBigNumber = (n: number) => numberFormat.format(Math.abs(n) >= 10 ? Math.round(n) : n);

export function HalfGauge({
  label,
  value,
  max,
  unit,
  requested,
  thresholds = DEFAULT_THRESHOLDS,
  size = "L",
  trend,
  trendLabel,
  state = "live",
  display = "percent",
  detail,
  note,
  updated,
  showHeader = size === "L",
  valueInside = false,
  onRetry,
  helpHref = METRICS_SERVER_DOCS,
  helpLabel = "Install metrics-server",
}: HalfGaugeProps) {
  const hasData = state === "live" || state === "stale";
  const fraction = fractionOf(value, max);
  const zone = zoneFor(fraction, thresholds);
  const percent = Math.round(fraction * 100);
  const requestedFraction = requested === undefined ? undefined : fractionOf(requested, max);

  const shown = useTween(hasData ? fraction : 0, { firstMs: FIRST_SWEEP_MS, updateMs: UPDATE_MS });
  const shownRequested = useTween(hasData ? (requestedFraction ?? 0) : 0, {
    firstMs: FIRST_SWEEP_MS,
    updateMs: UPDATE_MS,
  });

  const layout = layoutFor(size);
  const { spec, radius, innerRadius, cx, cy } = layout;
  const track = arcPath(radius, cx, cy);
  const knob = pointAt(shown, radius, cx, cy);
  const big = display === "percent" ? `${percent}%` : formatBigNumber(value);
  const small =
    state === "stale" && updated
      ? updated
      : (detail ?? (unit ? `${formatNumber(value)} of ${formatNumber(max)} ${unit}` : undefined));

  const hasTrend = trend !== undefined || Boolean(trendLabel);
  const showFooter =
    size === "L" && hasData && (requestedFraction !== undefined || Boolean(note) || hasTrend);

  const word = ZONE_WORDS[zone];
  const valueText = [`${label} ${percent} percent`, word, state === "stale" ? updated : undefined]
    .filter(Boolean)
    .join(", ");

  const meterProps = hasData
    ? {
        role: "meter",
        "aria-label": label,
        "aria-valuemin": 0,
        "aria-valuemax": max,
        "aria-valuenow": value,
        "aria-valuetext": valueText,
      }
    : {
        role: "img",
        "aria-label": `${label}: ${
          state === "loading" ? "loading" : state === "error" ? "couldn't load" : "no data"
        }`,
        "aria-busy": state === "loading" || undefined,
      };

  return (
    <div
      className={styles.gauge}
      data-size={size}
      data-state={state}
      data-zone={zone}
      data-value-inside={(size === "S" && valueInside) || undefined}
    >
      {showHeader && (
        <div className={styles.header}>
          <p className={styles.label}>{label}</p>
          <HeaderBadge state={state} zone={zone} />
        </div>
      )}

      <div className={styles.dial} {...meterProps}>
        <svg
          className={styles.svg}
          width={layout.width}
          height={layout.height}
          viewBox={`0 0 ${layout.width} ${layout.height}`}
          aria-hidden="true"
          focusable="false"
        >
          {hasData || state === "loading" ? (
            <path className={styles.track} d={track} strokeWidth={spec.stroke} />
          ) : (
            <path
              className={styles.dotted}
              d={track}
              strokeWidth={spec.dotStroke}
              pathLength={100}
              strokeDasharray="0 5.5"
            />
          )}

          {spec.tickLength > 0 &&
            hasData &&
            thresholds.map((t) => {
              const { from, to } = tickLine(t, layout);
              return (
                <line key={t} className={styles.tick} x1={from.x} y1={from.y} x2={to.x} y2={to.y} />
              );
            })}

          {hasData && shown > 0 && (
            <>
              <path
                className={styles.arcOutline}
                d={track}
                strokeWidth={spec.stroke + 3}
                pathLength={100}
                strokeDasharray={`${shown * 100} 100`}
              />
              <path
                className={styles.arc}
                d={track}
                strokeWidth={spec.stroke}
                pathLength={100}
                strokeDasharray={`${shown * 100} 100`}
              />
            </>
          )}

          {hasData && spec.innerStroke > 0 && shownRequested > 0 && (
            <path
              className={styles.requested}
              d={arcPath(innerRadius, cx, cy)}
              strokeWidth={spec.innerStroke}
              pathLength={100}
              strokeDasharray={`${shownRequested * 100} 100`}
            />
          )}

          {hasData && spec.knobRadius > 0 && (
            <circle
              className={styles.knob}
              cx={knob.x}
              cy={knob.y}
              r={spec.knobRadius}
              strokeWidth={spec.knobOutline}
            />
          )}
        </svg>

        <div className={styles.center}>
          {state === "loading" && (
            <>
              <span className={styles.skeletonValue} />
              {size !== "S" && <span className={styles.skeletonLine} />}
            </>
          )}
          {hasData && (
            <>
              <span className={styles.value}>{big}</span>
              {small && size === "L" && <span className={styles.detail}>{small}</span>}
            </>
          )}
          {state === "no-metrics" && <span className={styles.message}>No data</span>}
          {state === "error" && <span className={styles.message}>Couldn't load</span>}
        </div>
      </div>

      {size !== "S" && state === "no-metrics" && (
        <div className={styles.action}>
          <a href={helpHref} target="_blank" rel="noreferrer">
            {helpLabel}
          </a>
        </div>
      )}
      {size !== "S" && state === "error" && onRetry && (
        <div className={styles.action}>
          <Button variant="primary" size={size === "L" ? "md" : "sm"} onClick={onRetry}>
            Retry
          </Button>
        </div>
      )}

      {showFooter && (
        <div className={styles.footer}>
          <span className={styles.footerItem}>
            {requestedFraction !== undefined ? (
              <>
                <span className={styles.requestedKey} aria-hidden="true" />
                {Math.round(requestedFraction * 100)}% requested
              </>
            ) : (
              note
            )}
          </span>
          {hasTrend && (
            <span className={styles.footerItem}>
              <TrendIcon trend={trend} />
              {trendLabel ?? `${formatNumber(Math.abs((trend ?? 0) * 100))}% in 1h`}
            </span>
          )}
        </div>
      )}
    </div>
  );
}

function HeaderBadge({ state, zone }: { state: GaugeState; zone: GaugeZone }) {
  const badges: Record<Exclude<GaugeState, "live" | "loading">, [StatusTone, string]> = {
    stale: ["neutral", "Stale"],
    "no-metrics": ["neutral", "No data"],
    error: ["neutral", "Error"],
  };
  if (state === "loading") return null;
  const [tone, word] = state === "live" ? [zone, ZONE_WORDS[zone]] : badges[state];
  return <Badge tone={tone}>{word}</Badge>;
}

function TrendIcon({ trend }: { trend: number | undefined }) {
  const props = { size: 14, stroke: 1.75, "aria-hidden": true } as const;
  if (trend === undefined || trend === 0) return <IconMinus {...props} />;
  const up = trend > 0;
  return (
    <>
      {up ? <IconArrowUp {...props} /> : <IconArrowDown {...props} />}
      <span className="visually-hidden">{up ? "up" : "down"}</span>
    </>
  );
}
