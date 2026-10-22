import {
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
import { useNavigate } from "react-router";

import { cx } from "../lib/cx";
import { usePreferences } from "../lib/preferences";
import { useNamespaces } from "../lib/queries";
import { filterCommands, type Command } from "./commands";
import styles from "./CommandPalette.module.css";
import { allPages, deepSections, glanceSections, settingsSection } from "./navigation";
import { usePanel } from "./panel";

function useCommands(): Command[] {
  const navigate = useNavigate();
  const { mode, setMode, setNamespace, resolvedTheme, setTheme } = usePreferences();
  const panel = usePanel();
  const { data: namespaceList } = useNamespaces();
  const namespaces = namespaceList?.namespaces;

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
    return [...pages, ...actions, ...ns];
  }, [navigate, mode, setMode, panel, setNamespace, namespaces, resolvedTheme, setTheme]);
}

/** ⌘K palette: go to any page, switch mode, panel or namespace. */
export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const commands = useCommands();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const listId = useId();
  const results = filterCommands(commands, query);
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
              aria-label="Search pages and commands"
              aria-expanded="true"
              aria-controls={listId}
              aria-autocomplete="list"
              aria-activedescendant={current ? `${listId}-${current.id}` : undefined}
              placeholder="Go to a page, switch mode or namespace…"
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
                  <span className={styles.label}>{c.label}</span>
                  <span className={styles.group}>{c.group}</span>
                </li>
              );
            })}
          </ul>
          {results.length === 0 && <p className={styles.empty}>Nothing matches “{query}”.</p>}
        </div>
      )}
    </dialog>
  );
}
