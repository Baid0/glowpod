import { useId, useState } from "react";

import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Segmented } from "../../components/Segmented";
import {
  REFRESH_CHOICES,
  usePreferences,
  validThresholds,
  type Density,
  type Mode,
  type ThemeChoice,
} from "../../lib/preferences";
import { useClusterId, useOverview, useSession } from "../../lib/queries";
import styles from "./SettingsPage.module.css";

const THEMES = [
  { value: "dark", label: "Dark" },
  { value: "light", label: "Light" },
  { value: "system", label: "Match system" },
] as const satisfies readonly { value: ThemeChoice; label: string }[];

const DENSITIES = [
  { value: "comfortable", label: "Comfortable" },
  { value: "compact", label: "Compact" },
] as const satisfies readonly { value: Density; label: string }[];

const MODES = [
  { value: "glance", label: "Glance" },
  { value: "deep", label: "Deep" },
] as const satisfies readonly { value: Mode; label: string }[];

const REFRESH_LABELS: Record<(typeof REFRESH_CHOICES)[number], string> = {
  5000: "5 s",
  15000: "15 s",
  30000: "30 s",
  60000: "1 min",
  0: "Off",
};

/** Your preferences, saved in this browser. */
export function SettingsPage() {
  const prefs = usePreferences();
  return (
    <div className={styles.page}>
      <Card title="Appearance">
        <div className={styles.rows}>
          <Row label="Theme" hint="Dark is the default.">
            <Segmented
              label="Theme"
              options={THEMES}
              value={prefs.theme}
              onChange={prefs.setTheme}
            />
          </Row>
          <Row label="Density" hint="Compact fits more rows in tables.">
            <Segmented
              label="Density"
              options={DENSITIES}
              value={prefs.density}
              onChange={prefs.setDensity}
            />
          </Row>
          <Row
            label="View"
            hint="Glance keeps things simple; Deep shows everything. Nothing is locked away."
          >
            <Segmented label="View" options={MODES} value={prefs.mode} onChange={prefs.setMode} />
          </Row>
        </div>
      </Card>

      <Card title="Updates">
        <div className={styles.rows}>
          <Row
            label="Refresh gauges every"
            hint="Pods and events update live whatever you pick. Refreshing pauses while the tab is hidden."
          >
            <Segmented
              label="Refresh gauges every"
              options={REFRESH_CHOICES.map((ms) => ({ value: ms, label: REFRESH_LABELS[ms] }))}
              value={prefs.refreshMs}
              onChange={prefs.setRefreshMs}
            />
          </Row>
        </div>
      </Card>

      <ThresholdsCard />
      <AboutCard />
    </div>
  );
}

function Row({
  label,
  hint,
  children,
}: {
  label: string;
  hint: string;
  children: React.ReactNode;
}) {
  return (
    <div className={styles.row}>
      <div>
        <p className={styles.label}>{label}</p>
        <p className={styles.hint}>{hint}</p>
      </div>
      {children}
    </div>
  );
}

function ThresholdsCard() {
  const cluster = useClusterId();
  const { data } = useOverview();
  const { thresholdsFor, setThresholds } = usePreferences();
  const server = data?.thresholds ?? [0.7, 0.85];
  const mine = thresholdsFor(cluster);
  const current = mine ?? server;
  const [high, setHigh] = useState(String(Math.round(current[0] * 100)));
  const [critical, setCritical] = useState(String(Math.round(current[1] * 100)));
  const [saved, setSaved] = useState(false);
  const highId = useId();
  const criticalId = useId();
  const hintId = useId();
  const proposed: [number, number] = [Number(high) / 100, Number(critical) / 100];
  const valid = validThresholds(proposed);
  const name = data?.cluster.name ?? cluster;

  return (
    <Card title={`Gauge colors for ${name}`}>
      <form
        className={styles.thresholds}
        onSubmit={(e) => {
          e.preventDefault();
          if (!valid) return;
          setThresholds(cluster, proposed);
          setSaved(true);
        }}
      >
        <p className={styles.hint} id={hintId}>
          Gauges turn from lime to high at the first number and to critical at the second. They
          apply to this cluster, for you.
        </p>
        <div className={styles.fields}>
          <label htmlFor={highId} className={styles.field}>
            <span>High from</span>
            <span className={styles.percent}>
              <input
                id={highId}
                className={styles.input}
                inputMode="numeric"
                aria-describedby={hintId}
                aria-invalid={!valid}
                value={high}
                onChange={(e) => {
                  setHigh(e.target.value);
                  setSaved(false);
                }}
              />
              %
            </span>
          </label>
          <label htmlFor={criticalId} className={styles.field}>
            <span>Critical from</span>
            <span className={styles.percent}>
              <input
                id={criticalId}
                className={styles.input}
                inputMode="numeric"
                aria-describedby={hintId}
                aria-invalid={!valid}
                value={critical}
                onChange={(e) => {
                  setCritical(e.target.value);
                  setSaved(false);
                }}
              />
              %
            </span>
          </label>
        </div>
        {!valid && (
          <p className={styles.error} role="alert">
            Use two whole numbers between 1 and 99, the first smaller than the second.
          </p>
        )}
        <div className={styles.actions}>
          <Button type="submit" variant="primary" size="sm" disabled={!valid}>
            Save
          </Button>
          {mine && (
            <Button
              size="sm"
              onClick={() => {
                setThresholds(cluster, undefined);
                setHigh(String(Math.round(server[0] * 100)));
                setCritical(String(Math.round(server[1] * 100)));
                setSaved(false);
              }}
            >
              Use the defaults ({Math.round(server[0] * 100)}% / {Math.round(server[1] * 100)}%)
            </Button>
          )}
          {saved && (
            <span className={styles.hint} role="status">
              Saved.
            </span>
          )}
        </div>
      </form>
    </Card>
  );
}

function AboutCard() {
  const { data: session } = useSession();
  const { data } = useOverview();
  const cluster = session?.clusters[0];
  return (
    <Card title="About">
      <dl className={styles.about}>
        <div>
          <dt>Cluster</dt>
          <dd>{cluster?.name ?? "—"}</dd>
        </div>
        <div>
          <dt>API server</dt>
          <dd className={styles.mono}>{cluster?.server ?? "—"}</dd>
        </div>
        {data?.cluster.version && (
          <div>
            <dt>Kubernetes</dt>
            <dd>{data.cluster.version}</dd>
          </div>
        )}
        <div>
          <dt>Signed in as</dt>
          <dd className={styles.mono}>{session?.user.username ?? "—"}</dd>
        </div>
        <div>
          <dt>Mode</dt>
          <dd>
            {session?.mode === "in-cluster"
              ? "Installed in the cluster"
              : "Local, with your kubeconfig"}
          </dd>
        </div>
      </dl>
      <p className={styles.hint}>Settings are saved in this browser.</p>
    </Card>
  );
}
