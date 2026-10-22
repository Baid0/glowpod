import { Link } from "react-router";

import { Badge } from "../../../components/Badge";
import { Card } from "../../../components/Card";
import { bytes } from "../../../lib/format";
import { statusTone } from "../cells";
import { Chips, Fields } from "./Fields";
import { arr, at, num, record, selectorString, str } from "./read";
import { SelectedPods } from "./SelectedPods";
import styles from "./Summary.module.css";

interface KindProps {
  object: unknown;
  namespace: string;
}

function resourceLink(path: string, label: string) {
  return (
    <Link to={path} className={styles.mono}>
      {label}
    </Link>
  );
}

const nsPart = (ns: string) => (ns ? encodeURIComponent(ns) : "_");

export function ServiceSummary({ object, namespace }: KindProps) {
  const spec = at(object, "spec");
  const type = str(at(spec, "type")) ?? "ClusterIP";
  const selector = record(at(spec, "selector"));
  const external = [
    ...arr(at(object, "status", "loadBalancer", "ingress")).map(
      (i) => str(at(i, "ip")) ?? str(at(i, "hostname")) ?? "",
    ),
    ...arr(at(spec, "externalIPs")).map((v) => str(v) ?? ""),
  ].filter(Boolean);
  const ports = arr(at(spec, "ports"));
  return (
    <>
      <Card title="Service">
        <Fields
          rows={[
            ["Type", type],
            ["Cluster IP", str(at(spec, "clusterIP"))],
            [
              "External",
              type === "LoadBalancer" && external.length === 0
                ? "Waiting for an address…"
                : external.join(", "),
            ],
            ["External name", str(at(spec, "externalName"))],
          ]}
        />
        {ports.length > 0 && (
          <table className={styles.table}>
            <caption className="visually-hidden">Ports</caption>
            <thead>
              <tr>
                <th scope="col">Port</th>
                <th scope="col">Goes to</th>
                <th scope="col">Protocol</th>
                {type !== "ClusterIP" && <th scope="col">Node port</th>}
              </tr>
            </thead>
            <tbody>
              {ports.map((p, i) => (
                <tr key={i}>
                  <td className={styles.mono}>
                    {str(at(p, "port"))}
                    {str(at(p, "name")) ? ` (${str(at(p, "name")) ?? ""})` : ""}
                  </td>
                  <td className={styles.mono}>{str(at(p, "targetPort")) ?? str(at(p, "port"))}</td>
                  <td>{str(at(p, "protocol")) ?? "TCP"}</td>
                  {type !== "ClusterIP" && (
                    <td className={styles.mono}>{str(at(p, "nodePort"))}</td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      {Object.keys(selector).length > 0 ? (
        <SelectedPods
          title="Sends traffic to"
          namespace={namespace}
          selector={selectorString(selector)}
          empty="No pods match its selector, so it has nowhere to send traffic."
        />
      ) : (
        type !== "ExternalName" && (
          <Card title="Sends traffic to">
            <p className={styles.muted}>
              No selector: its endpoints are managed by hand or by another controller.
            </p>
          </Card>
        )
      )}
    </>
  );
}

export function IngressSummary({ object, namespace }: KindProps) {
  const spec = at(object, "spec");
  const tlsHosts = new Set(
    arr(at(spec, "tls")).flatMap((t) => arr(at(t, "hosts")).map((h) => str(h) ?? "")),
  );
  const rules = arr(at(spec, "rules")).flatMap((r) => {
    const host = str(at(r, "host")) ?? "*";
    return arr(at(r, "http", "paths")).map((p) => ({
      host,
      path: str(at(p, "path")) ?? "/",
      service: str(at(p, "backend", "service", "name")),
      port:
        str(at(p, "backend", "service", "port", "number")) ??
        str(at(p, "backend", "service", "port", "name")),
    }));
  });
  const addresses = arr(at(object, "status", "loadBalancer", "ingress"))
    .map((i) => str(at(i, "ip")) ?? str(at(i, "hostname")) ?? "")
    .filter(Boolean);
  return (
    <Card title="Routes">
      <Fields
        rows={[
          ["Class", str(at(spec, "ingressClassName"))],
          ["Address", addresses.length > 0 ? addresses.join(", ") : "Waiting for an address…"],
        ]}
      />
      {rules.length === 0 ? (
        <p className={styles.muted}>No rules: everything goes to the default backend.</p>
      ) : (
        <table className={styles.table}>
          <caption className="visually-hidden">Routes</caption>
          <thead>
            <tr>
              <th scope="col">Host</th>
              <th scope="col">Path</th>
              <th scope="col">Service</th>
            </tr>
          </thead>
          <tbody>
            {rules.map((r, i) => (
              <tr key={i}>
                <td className={styles.mono}>
                  {tlsHosts.has(r.host) ? "https://" : "http://"}
                  {r.host}
                </td>
                <td className={styles.mono}>{r.path}</td>
                <td>
                  {r.service
                    ? resourceLink(
                        `/resources/core/v1/services/${nsPart(namespace)}/${encodeURIComponent(r.service)}`,
                        `${r.service}${r.port ? ":" + r.port : ""}`,
                      )
                    : "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}

function quantity(v: unknown): string | undefined {
  const s = str(v);
  if (!s) return undefined;
  const m = /^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti|Pi)?$/.exec(s);
  if (!m?.[1]) return s;
  const mult = { Ki: 2 ** 10, Mi: 2 ** 20, Gi: 2 ** 30, Ti: 2 ** 40, Pi: 2 ** 50 }[m[2] ?? ""] ?? 1;
  return bytes(Number(m[1]) * mult);
}

export function ClaimSummary({ object }: KindProps) {
  const phase = str(at(object, "status", "phase")) ?? "Unknown";
  const volume = str(at(object, "spec", "volumeName"));
  const sc = str(at(object, "spec", "storageClassName"));
  return (
    <Card title="Volume claim" actions={<Badge tone={statusTone(phase)}>{phase}</Badge>}>
      <Fields
        rows={[
          ["Asks for", quantity(at(object, "spec", "resources", "requests", "storage"))],
          ["Has", quantity(at(object, "status", "capacity", "storage"))],
          [
            "Access",
            arr(at(object, "spec", "accessModes"))
              .map((m) => str(m))
              .join(", "),
          ],
          [
            "Storage class",
            sc &&
              resourceLink(
                `/resources/storage.k8s.io/v1/storageclasses/_/${encodeURIComponent(sc)}`,
                sc,
              ),
          ],
          [
            "Volume",
            volume &&
              resourceLink(
                `/resources/core/v1/persistentvolumes/_/${encodeURIComponent(volume)}`,
                volume,
              ),
          ],
        ]}
      />
      {phase === "Pending" && (
        <p className={styles.muted}>
          Pending: no volume is bound yet. Check the storage class can provision one, or that a
          matching volume exists.
        </p>
      )}
    </Card>
  );
}

export function VolumeSummary({ object }: KindProps) {
  const phase = str(at(object, "status", "phase")) ?? "Unknown";
  const claimNs = str(at(object, "spec", "claimRef", "namespace"));
  const claim = str(at(object, "spec", "claimRef", "name"));
  return (
    <Card title="Volume" actions={<Badge tone={statusTone(phase)}>{phase}</Badge>}>
      <Fields
        rows={[
          ["Size", quantity(at(object, "spec", "capacity", "storage"))],
          [
            "Access",
            arr(at(object, "spec", "accessModes"))
              .map((m) => str(m))
              .join(", "),
          ],
          ["When released", str(at(object, "spec", "persistentVolumeReclaimPolicy"))],
          ["Storage class", str(at(object, "spec", "storageClassName"))],
          [
            "Claimed by",
            claim &&
              resourceLink(
                `/resources/core/v1/persistentvolumeclaims/${nsPart(claimNs ?? "")}/${encodeURIComponent(claim)}`,
                `${claimNs ? claimNs + "/" : ""}${claim}`,
              ),
          ],
        ]}
      />
    </Card>
  );
}

export function StorageClassSummary({ object }: KindProps) {
  const isDefault =
    str(at(object, "metadata", "annotations", "storageclass.kubernetes.io/is-default-class")) ===
    "true";
  return (
    <Card title="Storage class" actions={isDefault ? <Badge tone="ok">Default</Badge> : undefined}>
      <Fields
        rows={[
          ["Provisioner", str(at(object, "provisioner"))],
          ["When released", str(at(object, "reclaimPolicy")) ?? "Delete"],
          ["Binding", str(at(object, "volumeBindingMode")) ?? "Immediate"],
          ["Can grow", str(at(object, "allowVolumeExpansion")) === "true" ? "Yes" : "No"],
        ]}
      />
    </Card>
  );
}

export function ConfigMapSummary({ object }: KindProps) {
  const data = record(at(object, "data"));
  const binary = Object.keys(record(at(object, "binaryData")));
  const keys = Object.entries(data);
  return (
    <Card title={`${String(keys.length + binary.length)} keys`}>
      {keys.length === 0 && binary.length === 0 ? (
        <p className={styles.muted}>No data.</p>
      ) : (
        <dl className={styles.values}>
          {keys.map(([k, v]) => (
            <div key={k}>
              <dt className={styles.mono}>{k}</dt>
              <dd>
                <pre className={styles.value}>{v}</pre>
              </dd>
            </div>
          ))}
          {binary.map((k) => (
            <div key={k}>
              <dt className={styles.mono}>{k}</dt>
              <dd className={styles.muted}>Binary data</dd>
            </div>
          ))}
        </dl>
      )}
    </Card>
  );
}

export function SecretSummary({ object }: KindProps) {
  const keys = Object.keys(record(at(object, "data")));
  return (
    <Card title="Secret">
      <Fields rows={[["Type", str(at(object, "type")) ?? "Opaque"]]} />
      <Chips items={keys} empty="No keys." />
      <p className={styles.muted}>Values stay hidden. Reveal them in the YAML tab.</p>
    </Card>
  );
}

export function WorkloadSummary({ object, namespace, resource }: KindProps & { resource: string }) {
  const spec = at(object, "spec");
  const status = at(object, "status");
  const daemon = resource === "daemonsets";
  const desired = daemon
    ? num(at(status, "desiredNumberScheduled"))
    : (num(at(spec, "replicas")) ?? 1);
  const ready = (daemon ? num(at(status, "numberReady")) : num(at(status, "readyReplicas"))) ?? 0;
  const updated =
    (daemon ? num(at(status, "updatedNumberScheduled")) : num(at(status, "updatedReplicas"))) ?? 0;
  const containers = arr(at(spec, "template", "spec", "containers"));
  const selector = record(at(spec, "selector", "matchLabels"));
  const tone =
    desired === 0 ? "neutral" : ready >= (desired ?? 0) ? "ok" : ready === 0 ? "critical" : "high";
  return (
    <>
      <Card
        title="Rollout"
        actions={
          <Badge tone={tone}>
            {ready} / {desired ?? 0} ready
          </Badge>
        }
      >
        <Fields
          rows={[
            ["Up to date", `${String(updated)} of ${String(desired ?? 0)}`],
            [
              "Strategy",
              str(at(spec, "strategy", "type")) ?? str(at(spec, "updateStrategy", "type")),
            ],
            [
              "Images",
              containers
                .map((c) => str(at(c, "image")))
                .filter(Boolean)
                .join(", "),
            ],
          ]}
        />
      </Card>
      {Object.keys(selector).length > 0 && (
        <SelectedPods
          title="Pods"
          namespace={namespace}
          selector={selectorString(selector)}
          empty="No pods right now."
        />
      )}
    </>
  );
}

export function CrdSummary({ object }: KindProps) {
  const group = str(at(object, "spec", "group")) ?? "";
  const plural = str(at(object, "spec", "names", "plural")) ?? "";
  const versions = arr(at(object, "spec", "versions"));
  const served = versions
    .filter((v) => at(v, "served") === true)
    .map((v) => str(at(v, "name")) ?? "");
  const storage = versions.find((v) => at(v, "storage") === true);
  const version = str(at(storage, "name")) ?? served[0];
  return (
    <Card
      title="Custom resource definition"
      actions={
        version && plural ? (
          <Link
            to={`/resources/${encodeURIComponent(group)}/${encodeURIComponent(version)}/${encodeURIComponent(plural)}`}
          >
            See all {plural}
          </Link>
        ) : undefined
      }
    >
      <Fields
        rows={[
          ["Kind", str(at(object, "spec", "names", "kind"))],
          ["Group", group],
          ["Versions", served.join(", ")],
          ["Scope", str(at(object, "spec", "scope"))],
          [
            "Short names",
            arr(at(object, "spec", "names", "shortNames"))
              .map((s) => str(s))
              .join(", "),
          ],
        ]}
      />
    </Card>
  );
}

export function NetworkPolicySummary({ object }: KindProps) {
  const spec = at(object, "spec");
  const target = selectorString(record(at(spec, "podSelector", "matchLabels")));
  const types = arr(at(spec, "policyTypes")).map((t) => str(t) ?? "");
  const describePeers = (rules: unknown[], dir: "from" | "to") =>
    rules.map((rule) => {
      const peers = arr(at(rule, dir)).map((p) => {
        const pods = selectorString(record(at(p, "podSelector", "matchLabels")));
        const nss = selectorString(record(at(p, "namespaceSelector", "matchLabels")));
        const cidr = str(at(p, "ipBlock", "cidr"));
        if (cidr) return cidr;
        if (pods && nss) return `pods ${pods} in namespaces ${nss}`;
        if (pods) return `pods ${pods}`;
        if (nss) return `namespaces ${nss}`;
        if (at(p, "namespaceSelector") !== undefined) return "every namespace";
        return "every pod in this namespace";
      });
      const ports = arr(at(rule, "ports"))
        .map((p) => `${str(at(p, "port")) ?? "any"}/${str(at(p, "protocol")) ?? "TCP"}`)
        .join(", ");
      return `${peers.length > 0 ? peers.join("; ") : "anywhere"}${ports ? ` on ${ports}` : ""}`;
    });
  const ingress = describePeers(arr(at(spec, "ingress")), "from");
  const egress = describePeers(arr(at(spec, "egress")), "to");
  return (
    <Card title="Policy">
      <Fields
        rows={[
          ["Applies to", target ? `pods ${target}` : "every pod in this namespace"],
          [
            "Incoming",
            types.includes("Ingress") || ingress.length > 0
              ? ingress.length > 0
                ? `Allowed from ${ingress.join(" · ")}`
                : "Blocked: nothing may connect in"
              : "Not restricted",
          ],
          [
            "Outgoing",
            types.includes("Egress")
              ? egress.length > 0
                ? `Allowed to ${egress.join(" · ")}`
                : "Blocked: nothing may connect out"
              : "Not restricted",
          ],
        ]}
      />
    </Card>
  );
}
