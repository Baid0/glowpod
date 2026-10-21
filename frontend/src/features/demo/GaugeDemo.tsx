import { IconRefresh } from "@tabler/icons-react";
import { useState, type ReactNode } from "react";

import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { HalfGauge, type GaugeState } from "../../components/HalfGauge";
import styles from "./GaugeDemo.module.css";

const states: { state: GaugeState; title: string; caption: ReactNode }[] = [
  { state: "loading", title: "Loading", caption: "Track + skeleton, no spinner" },
  { state: "live", title: "Live", caption: "Sweeps in 600 ms, eases 300 ms" },
  { state: "stale", title: "Stale", caption: "Grey arc, age under the value" },
  { state: "no-metrics", title: "No metrics", caption: "Dotted track + install link" },
  { state: "error", title: "Error", caption: "Dotted track + Retry" },
];

interface Sample {
  cpu: number;
  memory: number;
  pods: number;
  storage: number;
  network: number;
}

const initial: Sample = { cpu: 24.8, memory: 49.9, pods: 297, storage: 1.2, network: 120 };

function jitter(value: number, max: number): number {
  const next = value + (Math.random() - 0.5) * max * 0.3;
  return Math.round(Math.min(max, Math.max(0, next)) * 10) / 10;
}

/** Component sheet for HalfGauge, matching docs/design's gauge board. Dev builds only. */
export function GaugeDemo() {
  const [sample, setSample] = useState(initial);
  const [light, setLight] = useState(document.documentElement.dataset.theme === "light");

  const toggleTheme = () => {
    const next = !light;
    setLight(next);
    if (next) document.documentElement.dataset.theme = "light";
    else delete document.documentElement.dataset.theme;
  };

  const simulate = () => {
    setSample((s) => ({
      cpu: jitter(s.cpu, 40),
      memory: jitter(s.memory, 64),
      pods: Math.round(jitter(s.pods, 660)),
      storage: jitter(s.storage, 3.9),
      network: jitter(s.network, 200),
    }));
  };

  return (
    <main className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>Half-round gauge</h1>
        <p className={styles.legend}>Under 70% OK · 70–84% High · 85%+ Critical</p>
      </header>

      <div className={styles.toolbar}>
        <Button variant="primary" icon={<IconRefresh size={16} stroke={1.75} />} onClick={simulate}>
          Simulate update
        </Button>
        <Button onClick={toggleTheme} aria-pressed={light}>
          {light ? "Dark theme" : "Light theme"}
        </Button>
      </div>

      <div className={styles.topRow}>
        <Section title="Sizes">
          <div className={styles.row}>
            <Labelled caption="L · 220 px · overview">
              <HalfGauge
                label="CPU"
                value={sample.cpu}
                max={40}
                unit="cores"
                requested={30}
                showHeader={false}
              />
            </Labelled>
            <Labelled caption="M · 140 px · node cards">
              <HalfGauge label="CPU" value={sample.cpu} max={40} size="M" />
            </Labelled>
            <Labelled caption="S · 64 px · tables">
              <HalfGauge label="CPU" value={sample.cpu} max={40} size="S" />
            </Labelled>
          </div>
        </Section>

        <Section title="Zones">
          <div className={styles.row}>
            {[45, 78, 92].map((v) => (
              <HalfGauge key={v} label={`Zone ${v}`} value={v} max={100} size="M" />
            ))}
          </div>
          <div className={styles.row}>
            <Badge tone="ok">OK</Badge>
            <Badge tone="high">High</Badge>
            <Badge tone="critical">Critical</Badge>
          </div>
        </Section>
      </div>

      <Section title="States">
        <div className={styles.states}>
          {states.map(({ state, title, caption }) => (
            <div key={state} className={styles.tile}>
              <HalfGauge
                label={`CPU (${title})`}
                value={24.8}
                max={40}
                size="L"
                showHeader={false}
                state={state}
                detail="updated 12 s ago"
                updated="updated 3 min ago"
                onRetry={() => undefined}
              />
              <p className={styles.tileTitle}>{title}</p>
              <p className={styles.caption}>{caption}</p>
            </div>
          ))}
        </div>
      </Section>

      <Section title="Overview cards">
        <div className={styles.overview}>
          <Card padding="gauge">
            <HalfGauge
              label="CPU"
              value={sample.cpu}
              max={40}
              unit="cores"
              requested={30}
              trend={0.04}
            />
          </Card>
          <Card padding="gauge">
            <HalfGauge
              label="Memory"
              value={sample.memory}
              max={64}
              unit="GiB"
              requested={44.8}
              trend={0.09}
            />
          </Card>
          <Card padding="gauge">
            <HalfGauge
              label="Pods"
              value={sample.pods}
              max={660}
              unit="pods"
              note="6 nodes × 110 max"
              trendLabel="12 in 1h"
              trend={0.02}
            />
          </Card>
          <Card padding="gauge">
            <HalfGauge
              label="Storage"
              value={sample.storage}
              max={3.9}
              unit="TiB"
              note="18 volumes"
              trend={0.004}
            />
          </Card>
          <Card padding="gauge">
            <HalfGauge
              label="Network"
              value={sample.network}
              max={200}
              display="value"
              detail="MB/s in + out"
              note="of 7-day peak 200"
            />
          </Card>
        </div>
      </Section>

      <Section title="Buttons">
        <div className={styles.row}>
          <Button variant="primary">Primary</Button>
          <Button>Secondary</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="danger">Delete</Button>
          <Button size="sm">View</Button>
          <Button disabled>Disabled</Button>
        </div>
      </Section>
    </main>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className={styles.section} aria-label={title}>
      <h2 className={styles.sectionTitle}>{title}</h2>
      {children}
    </section>
  );
}

function Labelled({ caption, children }: { caption: string; children: ReactNode }) {
  return (
    <figure className={styles.labelled}>
      {children}
      <figcaption className={styles.caption}>{caption}</figcaption>
    </figure>
  );
}
