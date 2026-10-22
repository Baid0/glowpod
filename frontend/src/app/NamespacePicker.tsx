import { IconCheck, IconChevronDown, IconSearch, IconStack2 } from "@tabler/icons-react";
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";

import { cx } from "../lib/cx";
import { usePreferences } from "../lib/preferences";
import { useNamespaces } from "../lib/queries";
import { filterNamespaces } from "./namespaces";
import styles from "./NamespacePicker.module.css";

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;
/** Matches the popover's width in the CSS. */
const POPOVER_WIDTH = 300;

interface Option {
  /** "" is all namespaces. */
  value: string;
  kind: "all" | "namespace" | "use";
}

/**
 * The top bar's namespace picker: plain text that opens a searchable list.
 * People who may not list namespaces get no "All namespaces" and can use
 * any name they type.
 */
export function NamespacePicker() {
  const { namespace, setNamespace } = usePreferences();
  const { data } = useNamespaces();
  const names = useMemo(() => data?.namespaces ?? [], [data]);
  const restricted = data?.restricted ?? false;
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  // Open toward whichever side has room: the picker sits near the right edge
  // on wide screens and at the left on phones.
  const [align, setAlign] = useState<"left" | "right">("left");
  const box = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const listId = useId();

  // Someone who may not list namespaces can't see "all" either: start them
  // in one they can.
  const first = names[0];
  useEffect(() => {
    if (restricted && namespace === "" && first) setNamespace(first);
  }, [restricted, namespace, first, setNamespace]);

  const options = useMemo<Option[]>(() => {
    const q = query.trim().toLowerCase();
    // Keep a saved namespace pickable even if it's gone or not loaded yet.
    const all = namespace && !names.includes(namespace) ? [namespace, ...names] : names;
    const out: Option[] = [];
    if (!restricted && (!q || "all namespaces".includes(q))) out.push({ value: "", kind: "all" });
    for (const n of filterNamespaces(all, q)) out.push({ value: n, kind: "namespace" });
    // A name that isn't listed: always for people who can't list namespaces,
    // otherwise only when nothing matches (it may have just been created).
    const matched = out.some((o) => o.kind === "namespace");
    if (q && DNS_LABEL.test(q) && !all.includes(q) && (restricted || !matched)) {
      out.push({ value: q, kind: "use" });
    }
    return out;
  }, [names, namespace, query, restricted]);

  const close = (refocus: boolean) => {
    setOpen(false);
    setQuery("");
    if (refocus) trigger.current?.focus();
  };
  const show = () => {
    const current = options.findIndex((o) => o.kind !== "use" && o.value === namespace);
    setActive(Math.max(0, current));
    const rect = trigger.current?.getBoundingClientRect();
    setAlign(rect && rect.left + POPOVER_WIDTH > window.innerWidth - 16 ? "right" : "left");
    setOpen(true);
  };
  const pick = (o: Option | undefined) => {
    if (!o) return;
    setNamespace(o.value);
    close(true);
  };

  // Click outside closes.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!box.current?.contains(e.target as Node)) close(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("mousedown", onDown);
    };
  }, [open]);

  // Keep the active option in view while moving with the keyboard.
  useEffect(() => {
    if (!open) return;
    const el = list.current?.querySelector<HTMLElement>(`[data-index="${String(active)}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [active, open]);

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    const last = options.length - 1;
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        setActive((i) => (i >= last ? 0 : i + 1));
        break;
      case "ArrowUp":
        e.preventDefault();
        setActive((i) => (i <= 0 ? last : i - 1));
        break;
      case "Home":
        e.preventDefault();
        setActive(0);
        break;
      case "End":
        e.preventDefault();
        setActive(last);
        break;
      case "Enter":
        e.preventDefault();
        pick(options[Math.min(active, last)]);
        break;
      case "Escape":
        e.preventDefault();
        close(true);
        break;
      case "Tab":
        close(false);
        break;
    }
  };

  const label = namespace || (restricted ? "Pick a namespace" : "All namespaces");
  const current = options[Math.min(active, options.length - 1)];
  const count = names.length;

  return (
    <div ref={box} className={styles.picker}>
      <button
        ref={trigger}
        type="button"
        className={styles.trigger}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={`Namespace: ${label}`}
        onClick={() => {
          if (open) close(false);
          else show();
        }}
        onKeyDown={(e) => {
          if (!open && e.key === "ArrowDown") {
            e.preventDefault();
            show();
          }
        }}
      >
        <span className={styles.value}>{label}</span>
        <IconChevronDown size={16} stroke={1.75} aria-hidden="true" className={styles.chevron} />
      </button>

      {open && (
        <div className={styles.popover} data-align={align}>
          <div className={styles.search}>
            <IconSearch size={16} stroke={1.75} aria-hidden="true" />
            <input
              className={styles.input}
              role="combobox"
              aria-label="Search namespaces"
              aria-expanded="true"
              aria-controls={listId}
              aria-autocomplete="list"
              aria-activedescendant={current ? `${listId}-${String(active)}` : undefined}
              placeholder={restricted ? "Type a namespace…" : "Search namespaces…"}
              autoFocus
              autoComplete="off"
              spellCheck={false}
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActive(0);
              }}
              onKeyDown={onKey}
            />
          </div>
          <ul ref={list} id={listId} className={styles.list} role="listbox" aria-label="Namespaces">
            {options.map((o, i) => {
              const selected = o.kind !== "use" && o.value === namespace;
              return (
                <li
                  key={o.kind + o.value}
                  id={`${listId}-${String(i)}`}
                  data-index={i}
                  role="option"
                  aria-selected={selected}
                  className={cx(styles.option, i === active && styles.active)}
                  onMouseMove={() => {
                    setActive(i);
                  }}
                  onMouseDown={(e) => {
                    e.preventDefault(); // keep focus in the search field
                  }}
                  onClick={() => {
                    pick(o);
                  }}
                >
                  <span className={styles.check} aria-hidden="true">
                    {selected && <IconCheck size={16} stroke={2} />}
                    {o.kind === "all" && !selected && <IconStack2 size={16} stroke={1.75} />}
                  </span>
                  {o.kind === "all" ? (
                    <span className={styles.label}>All namespaces</span>
                  ) : o.kind === "use" ? (
                    <span className={styles.label}>
                      Use <span className={styles.mono}>{o.value}</span>
                    </span>
                  ) : (
                    <span className={cx(styles.label, styles.mono)}>
                      <Highlight text={o.value} query={query} />
                    </span>
                  )}
                </li>
              );
            })}
          </ul>
          {options.length === 0 && (
            <p className={styles.empty}>
              {query.trim() && !DNS_LABEL.test(query.trim().toLowerCase())
                ? "Namespace names use lowercase letters, digits and dashes."
                : `No namespace matches “${query}”.`}
            </p>
          )}
          <p className={styles.footer}>
            <span>
              {restricted
                ? "You can't list namespaces here; type one you can use."
                : `${String(count)} ${count === 1 ? "namespace" : "namespaces"}`}
            </span>
            <span className={styles.keys} aria-hidden="true">
              ↑↓ · Enter · Esc
            </span>
          </p>
        </div>
      )}
    </div>
  );
}

/** The name with the matching part in ink, the rest in body. */
function Highlight({ text, query }: { text: string; query: string }) {
  const q = query.trim().toLowerCase();
  const at = q ? text.indexOf(q) : -1;
  if (at < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, at)}
      <mark className={styles.match}>{text.slice(at, at + q.length)}</mark>
      {text.slice(at + q.length)}
    </>
  );
}
