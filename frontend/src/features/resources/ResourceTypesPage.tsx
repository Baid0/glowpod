import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { api } from "../../lib/api";
import { useClusterId } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import type { ResourceType } from "../../lib/types";
import { isBuiltInGroup } from "./cells";
import styles from "./ResourceTypesPage.module.css";
import { Link } from "../../lib/clusterLinks";

/** Every resource type the cluster serves, grouped by API group. */
export function ResourceTypesPage({ only }: { only?: "custom" }) {
  const cluster = useClusterId();
  const [search, setSearch] = useState("");
  const { data, error, isPending } = useQuery({
    queryKey: queryKeys.resourceTypes(cluster),
    queryFn: ({ signal }) => api.resourceTypes(cluster, signal),
    staleTime: 60_000,
  });

  const groups = useMemo(() => {
    const words = search.toLowerCase().split(/\s+/).filter(Boolean);
    const byGroup = new Map<string, ResourceType[]>();
    for (const t of data ?? []) {
      if (only === "custom" && isBuiltInGroup(t.group)) continue;
      const hay =
        `${t.kind} ${t.resource} ${t.group} ${(t.shortNames ?? []).join(" ")}`.toLowerCase();
      if (!words.every((w) => hay.includes(w))) continue;
      const list = byGroup.get(t.group) ?? [];
      list.push(t);
      byGroup.set(t.group, list);
    }
    return [...byGroup.entries()];
  }, [data, search, only]);

  return (
    <div className={styles.page}>
      <input
        type="search"
        className={styles.search}
        aria-label="Search resource types"
        placeholder="Search kinds, e.g. Application or cert"
        value={search}
        onChange={(e) => {
          setSearch(e.target.value);
        }}
      />
      {error && <p className={styles.note}>{error.message}</p>}
      {isPending && <div className={styles.skeleton} />}
      {data && groups.length === 0 && (
        <p className={styles.note}>
          {only === "custom" && !search
            ? "No custom resources installed yet."
            : `Nothing matches “${search}”.`}
        </p>
      )}
      {groups.map(([group, types]) => (
        <section key={group || "core"} className={styles.group} aria-label={group || "core"}>
          <h2 className={styles.groupTitle}>{group || "core"}</h2>
          <ul className={styles.list}>
            {types.map((t) => (
              <li key={t.resource}>
                <Link
                  className={styles.type}
                  to={`/resources/${encodeURIComponent(t.group || "core")}/${encodeURIComponent(t.version)}/${encodeURIComponent(t.resource)}`}
                >
                  <span className={styles.kind}>{t.kind}</span>
                  <span className={styles.meta}>
                    {t.resource}
                    {t.shortNames?.length ? ` · ${t.shortNames.join(", ")}` : ""} · {t.version}
                    {t.namespaced ? "" : " · cluster-wide"}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
