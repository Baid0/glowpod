import {
  IconCloud,
  IconEye,
  IconFolder,
  IconLayoutSidebarLeftCollapse,
  IconLayoutSidebarLeftExpand,
  IconMoon,
  IconSearch,
  IconStack2,
  IconSun,
} from "@tabler/icons-react";
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";

import { cx } from "../lib/cx";
import { usePreferences } from "../lib/preferences";
import { useClusterId, useClusters, useNamespaces } from "../lib/queries";
import { filterCommands, type Command } from "./commands";
import styles from "./CommandPalette.module.css";
import { allPages, deepSections, glanceSections, settingsSection } from "./navigation";
import { usePanel } from "./panel";
import { useNavigate } from "../lib/clusterPaths";
import { hitIcon, hitPath, MIN_QUERY, useDebounced, useSearch } from "../features/search/search";

function useCommands(home: boolean): Command[] {
  const navigate = useNavigate();
  const { mode, setMode, setNamespace, resolvedTheme, setTheme } = usePreferences();
  const panel = usePanel();
  const { data: namespaceList } = useNamespaces();
  const namespaces = namespaceList?.namespaces;
  const { data: clusters } = useClusters();
  const clusterId = useClusterId();

  return useMemo(() => {
    const iconFor = (path: string) =>
      [...deepSections, ...glanceSections, settingsSection].find((s) =>
        s.items.some((i) => i.path === path),
      )?.icon;

    const pages: Command[] = allPages().map((p) => {
      const icon = iconFor(p.path);
      return {
        id: "page:" + p.path,
        label: p.label,
        group: "Go to",
        keywords: p.path.replaceAll("/", " "),
        ...(icon ? { icon } : {}),
        run: () => void navigate(p.path),
      };
    });
    const other = mode === "deep" ? "glance" : "deep";
    const actions: Command[] = [
      {
        id: "mode",
        label: other === "glance" ? "Switch to Glance mode" : "Switch to Deep mode",
        group: "View",
        icon: IconEye,
        keywords: "simple advanced",
        run: () => {
          setMode(other);
        },
      },
      {
        id: "panel",
        label: panel.open ? "Collapse the side panel" : "Expand the side panel",
        group: "View",
        icon: panel.open ? IconLayoutSidebarLeftCollapse : IconLayoutSidebarLeftExpand,
        keywords: "sidebar",
        run: () => {
          panel.setOpen(!panel.open);
        },
      },
      {
        id: "theme",
        label: resolvedTheme === "dark" ? "Switch to the light theme" : "Switch to the dark theme",
        group: "View",
        icon: resolvedTheme === "dark" ? IconSun : IconMoon,
        keywords: "theme appearance color",
        run: () => {
          setTheme(resolvedTheme === "dark" ? "light" : "dark");
        },
      },
      {
        id: "ns:",
        label: "All namespaces",
        group: "Namespace",
        icon: IconStack2,
        run: () => {
          setNamespace("");
        },
      },
    ];
    const ns: Command[] = (namespaces ?? []).map((name) => ({
      id: "ns:" + name,
      label: name,
      group: "Namespace",
      icon: IconFolder,
      run: () => {
        setNamespace(name);
      },
    }));
    const others: Command[] = (clusters ?? [])
      .filter((c) => home || c.id !== clusterId)
      .map((c) => ({
        id: "cluster:" + c.id,
        label: c.name,
        group: "Cluster",
        icon: IconCloud,
        keywords: `switch context ${c.server}`,
        run: () => void navigate(`/c/${encodeURIComponent(c.id)}/`),
      }));
    // The Clusters page has no cluster: only other clusters and the theme.
    if (home) return [...others, ...actions.filter((a) => a.id === "theme")];
    return [...pages, ...actions, ...ns, ...others];
  }, [
    home,
    navigate,
    mode,
    setMode,
    panel,
    setNamespace,
    namespaces,
    resolvedTheme,
    setTheme,
    clusters,
    clusterId,
  ]);
}

/**
 * Objects whose names match query, in every cluster: the one on screen first.
 * Returns them as commands that open each object, and a status line.
 */
function useFound(query: string, open: boolean): { hits: Command[]; status: string } {
  const navigate = useNavigate();
  const { data: clusters } = useClusters();
  const clusterId = useClusterId();
  const ordered = [...(clusters ?? [])].sort(
    (a, b) => Number(b.id === clusterId) - Number(a.id === clusterId),
  );
  const searches = useSearch(open ? ordered : [], useDebounced(query));
  const many = (clusters?.length ?? 0) > 1;

  const hits = searches.flatMap(({ cluster, data }) =>
    (data?.hits ?? []).map((h): Command => ({
      id: `hit:${cluster.id}:${h.group}/${h.resource}/${h.namespace ?? ""}/${h.name}`,
      label: h.name,
      detail: [h.namespace, many ? cluster.name : undefined].filter(Boolean).join(" · "),
      group: h.kind,
      icon: hitIcon(h),
      run: () => void navigate(hitPath(cluster.id, h)),
    })),
  );

  const pending = searches.filter((s) => s.pending).length;
  const failed = searches.filter((s) => s.error && !s.data).map((s) => s.cluster.name);
  const more = searches.filter((s) => s.data?.more).map((s) => s.cluster.name);
  // People who can't list across namespaces are searched in the ones they picked.
  const denied = searches.filter((s) => s.data?.denied).map((s) => s.cluster.name);
  const status = [
    pending > 0 &&
      (many
        ? `Searching ${String(pending)} ${pending === 1 ? "cluster" : "clusters"}…`
        : "Searching…"),
    failed.length > 0 && `Couldn't search ${failed.join(", ")}.`,
    denied.length > 0 &&
      `To search ${denied.join(", ")}, open ${denied.length === 1 ? "it" : "each"} and pick your namespace first.`,
    more.length > 0 &&
      (many
        ? `More matches in ${more.join(", ")}: type more of the name.`
        : "More matches: type more of the name."),
  ]
    .filter(Boolean)
    .join(" ");
  return { hits, status };
}

/**
 * ⌘K palette: go to any page, switch mode, panel or namespace, and find
 * objects by name in every cluster. On the Clusters page (home) there's no
 * cluster to go into, so it offers clusters and objects.
 */
export function CommandPalette({
  open,
  onClose,
  home = false,
}: {
  open: boolean;
  onClose: () => void;
  home?: boolean;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const commands = useCommands(home);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const listId = useId();
  const statusId = useId();
  const found = useFound(query, open);
  const searching = query.trim().length >= MIN_QUERY;
  const results = [...filterCommands(commands, query), ...(searching ? found.hits : [])];
  const current = results[Math.min(active, results.length - 1)];

  useEffect(() => {
    const d = dialog.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);

  const run = (c: Command | undefined) => {
    if (!c) return;
    onClose();
    c.run();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const step = e.key === "ArrowDown" ? 1 : -1;
      setActive(
        (i) =>
          (Math.min(i, results.length - 1) + step + results.length) % Math.max(results.length, 1),
      );
    } else if (e.key === "Enter") {
      e.preventDefault();
      run(current);
    }
  };

  return (
    <dialog
      ref={dialog}
      className={styles.dialog}
      aria-label="Search and commands"
      onClose={() => {
        setQuery("");
        setActive(0);
        onClose();
      }}
      onClick={(e) => {
        if (e.target === dialog.current) onClose(); // backdrop
      }}
    >
      {open && (
        <div className={styles.box}>
          <div className={styles.inputRow}>
            <IconSearch size={18} stroke={1.75} aria-hidden="true" />
            <input
              className={styles.input}
              role="combobox"
              aria-label="Search pages, commands and objects"
              aria-expanded="true"
              aria-controls={listId}
              aria-autocomplete="list"
              aria-activedescendant={current ? `${listId}-${current.id}` : undefined}
              aria-describedby={statusId}
              placeholder={
                home
                  ? "Find a pod, service, secret… or a cluster"
                  : "Go to a page, or find an object by name…"
              }
              autoFocus
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActive(0);
              }}
              onKeyDown={onKeyDown}
            />
            <kbd className={styles.kbd}>Esc</kbd>
          </div>
          <ul id={listId} className={styles.list} role="listbox" aria-label="Results">
            {results.map((c, i) => {
              const Icon = c.icon;
              const selected = c === current;
              return (
                <li
                  key={c.id}
                  id={`${listId}-${c.id}`}
                  role="option"
                  aria-label={[c.label, c.detail, c.group].filter(Boolean).join(", ")}
                  aria-selected={selected}
                  className={cx(styles.option, selected && styles.selected)}
                  onMouseMove={() => {
                    setActive(i);
                  }}
                  onClick={() => {
                    run(c);
                  }}
                >
                  {Icon && <Icon size={16} stroke={1.75} aria-hidden="true" />}
                  <span className={styles.label}>
                    {c.label}
                    {c.detail && <span className={styles.detail}> {c.detail}</span>}
                  </span>
                  <span className={styles.group}>{c.group}</span>
                </li>
              );
            })}
          </ul>
          {results.length === 0 && !(searching && found.status.startsWith("Searching")) && (
            <p className={styles.empty}>Nothing matches “{query}”.</p>
          )}
          <p id={statusId} className={styles.status} role="status">
            {searching ? found.status : ""}
          </p>
        </div>
      )}
    </dialog>
  );
}
