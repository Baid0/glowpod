import { IconArrowDown, IconArrowUp, IconSelector, IconX } from "@tabler/icons-react";
import {
  createColumnHelper,
  createSortedRowModel,
  rowSortingFeature,
  tableFeatures,
  useTable,
  type SortingState,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useMemo, useRef, useState, type SyntheticEvent } from "react";

import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { ApiError } from "../../lib/api";
import { cx } from "../../lib/cx";
import { describeNamespaces, usePreferences } from "../../lib/preferences";
import { CreateButtons } from "../create/CreateButtons";
import type { ResourceRef, TableColumn, TableRow } from "../../lib/types";
import {
  columnsFor,
  compareRows,
  detailPath,
  formatCell,
  isAgeColumn,
  isNumeric,
  isStatusColumn,
  matchesSearch,
  nameColumn,
  statusTone,
} from "./cells";
import styles from "./ResourceListPage.module.css";
import { useResourceList } from "./useResourceList";
import { Link } from "../../lib/clusterLinks";

const features = tableFeatures({ rowSortingFeature, sortedRowModel: createSortedRowModel() });
const helper = createColumnHelper<typeof features, TableRow>();
const noRows: TableRow[] = [];
/** Row heights for the density setting; base.css sets --row-height to match. */
const ROW_HEIGHT = { comfortable: 40, compact: 32 } as const;

/** Synthetic column: the API's Table omits the namespace across all namespaces. */
const NAMESPACE_COLUMN: TableColumn = { name: "Namespace", type: "string", priority: 0 };

/**
 * Any resource as a kubectl-style list: the API's own columns, sorting,
 * search over loaded rows, label selectors, paging and live refresh.
 */
export function ResourceListPage({ resource }: { resource: ResourceRef }) {
  const { mode, namespace, namespaces, density } = usePreferences();
  const [search, setSearch] = useState("");
  const [selectorDraft, setSelectorDraft] = useState("");
  const [selector, setSelector] = useState("");
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());
  const [sorting, setSorting] = useState<SortingState>([]);
  const { query, live } = useResourceList(resource, namespace, selector);
  const { data, error, isPending, hasNextPage, isFetchingNextPage, fetchNextPage, refetch } = query;

  const first = data?.pages[0];
  const type = first?.type;
  const allRows = useMemo(() => data?.pages.flatMap((p) => p.rows) ?? noRows, [data]);
  const rows = useMemo(
    () => (search ? allRows.filter((r) => matchesSearch(r, search)) : allRows),
    [allRows, search],
  );
  // One namespace picked: every row is in it. Otherwise say which.
  const showNamespace = Boolean(type?.namespaced && namespaces.length !== 1);
  const skipped = first?.skipped ?? [];
  const apiColumns = first?.columns;

  const columns = useMemo(() => {
    if (!apiColumns) return [];
    const nameIdx = nameColumn(apiColumns);
    const shown = columnsFor(apiColumns, mode, hidden);
    const defs = shown.flatMap((i) => {
      const col = apiColumns[i];
      if (!col) return [];
      return helper.accessor((row) => row.cells[i], {
        id: `c${i}`,
        header: col.name,
        sortFn: (a, b) => compareRows(a.original, b.original, i, col),
        cell: (ctx) => (
          <Cell
            value={ctx.getValue()}
            column={col}
            row={ctx.row.original}
            isName={i === nameIdx}
            resource={resource}
          />
        ),
      });
    });
    if (showNamespace) {
      const at = shown.indexOf(nameIdx) + 1;
      defs.splice(
        at,
        0,
        helper.accessor((row): unknown => row.object.namespace ?? "", {
          id: "namespace",
          header: NAMESPACE_COLUMN.name,
          sortFn: (a, b) =>
            (a.original.object.namespace ?? "").localeCompare(b.original.object.namespace ?? ""),
          cell: (ctx) => <span className={styles.muted}>{formatCell(ctx.getValue())}</span>,
        }),
      );
    }
    return defs;
  }, [apiColumns, mode, hidden, showNamespace, resource]);

  const table = useTable({
    features,
    columns,
    data: rows,
    getRowId: (row) => row.object.uid || `${row.object.namespace ?? ""}/${row.object.name}`,
    state: { sorting },
    onSortingChange: setSorting,
  });
  const tableRows = table.getRowModel().rows;

  const scroller = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: tableRows.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => ROW_HEIGHT[density],
    overscan: 12,
    initialRect: { width: 1200, height: 800 },
  });
  const items = virtualizer.getVirtualItems();
  const last = items.at(-1);
  const nearEnd = last !== undefined && last.index >= tableRows.length - 10;
  // Infinite scroll: fetch the next page as the last loaded rows come into view.
  useEffect(() => {
    if (nearEnd && hasNextPage && !isFetchingNextPage && !search) void fetchNextPage();
  }, [nearEnd, hasNextPage, isFetchingNextPage, search, fetchNextPage]);
  const padTop = items[0]?.start ?? 0;
  const padBottom = last ? virtualizer.getTotalSize() - last.end : 0;

  const where =
    namespaces.length > 0 && type?.namespaced !== false
      ? ` in ${describeNamespaces(namespaces)}`
      : "";
  const word = resource.resource;
  const remaining = data?.pages.at(-1)?.remainingItemCount;

  const applySelector = (e: SyntheticEvent) => {
    e.preventDefault();
    setSelector(selectorDraft.trim());
  };

  return (
    <section className={styles.card} aria-label={word}>
      <div className={styles.toolbar}>
        <input
          type="search"
          className={styles.search}
          aria-label={`Search loaded ${word}`}
          placeholder={`Search ${word}…`}
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
          }}
        />
        {mode === "deep" && (
          <form className={styles.selectorForm} onSubmit={applySelector}>
            <input
              className={cx(styles.search, styles.selector)}
              aria-label="Label selector"
              placeholder="Label selector, e.g. app=web"
              value={selectorDraft}
              onChange={(e) => {
                setSelectorDraft(e.target.value);
              }}
            />
            {selector && (
              <button
                type="button"
                className={styles.clear}
                aria-label="Clear label selector"
                onClick={() => {
                  setSelector("");
                  setSelectorDraft("");
                }}
              >
                <IconX size={14} stroke={1.75} aria-hidden="true" />
              </button>
            )}
          </form>
        )}
        {mode === "deep" && apiColumns && apiColumns.some((c) => c.priority > 0) && (
          <details className={styles.columns}>
            <summary>Columns</summary>
            <div className={styles.columnsMenu}>
              {apiColumns
                .filter((c) => c.priority > 0)
                .map((c) => (
                  <label key={c.name} className={styles.columnOption}>
                    <input
                      type="checkbox"
                      checked={!hidden.has(c.name)}
                      onChange={(e) => {
                        const next = new Set(hidden);
                        if (e.target.checked) next.delete(c.name);
                        else next.add(c.name);
                        setHidden(next);
                      }}
                    />
                    {c.name}
                  </label>
                ))}
            </div>
          </details>
        )}
        <span className={styles.summary} aria-live="polite">
          {data && (
            <>
              {search ? `${rows.length} of ${allRows.length}` : allRows.length} {word}
              {remaining ? ` · ${remaining} more` : ""}
            </>
          )}
        </span>
        <span className={styles.live} data-live={live}>
          <span className={styles.liveDot} aria-hidden="true" />
          {live ? "Live" : "Not live"}
        </span>
        <CreateButtons
          resource={resource}
          type={type}
          namespaces={namespaces}
          deep={mode === "deep"}
        />
      </div>

      {type && !type.namespaced && namespace && (
        <p className={styles.note}>
          {type.kind} objects aren't in a namespace, so this shows all of them.
        </p>
      )}
      {skipped.length > 0 && (
        <p className={styles.note} role="status">
          You can't list {word} in {skipped.join(", ")}, so{" "}
          {skipped.length === 1 ? "it's" : "they're"} left out.
        </p>
      )}

      {error ? (
        <ListError
          error={error}
          word={word}
          where={where}
          selector={selector}
          onRetry={() => void refetch()}
        />
      ) : isPending ? (
        <div className={styles.skeleton} aria-label={`Loading ${word}`} />
      ) : rows.length === 0 ? (
        <p className={styles.empty}>
          {search
            ? `Nothing matches “${search}” in the loaded ${word}.`
            : selector
              ? `No ${word}${where} match ${selector}.`
              : `No ${word}${where} yet. Nothing growing here.`}
        </p>
      ) : (
        <div ref={scroller} className={styles.scroller}>
          <table className={styles.table}>
            <thead>
              {table.getHeaderGroups().map((group) => (
                <tr key={group.id}>
                  {group.headers.map((header) => {
                    const sorted = header.column.getIsSorted();
                    const Icon =
                      sorted === "asc"
                        ? IconArrowUp
                        : sorted === "desc"
                          ? IconArrowDown
                          : IconSelector;
                    return (
                      <th
                        key={header.id}
                        scope="col"
                        aria-sort={
                          sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : "none"
                        }
                      >
                        <button
                          type="button"
                          className={styles.sort}
                          onClick={header.column.getToggleSortingHandler()}
                        >
                          <table.FlexRender header={header} />
                          <Icon
                            size={14}
                            stroke={1.75}
                            aria-hidden="true"
                            className={styles.sortIcon}
                            data-active={Boolean(sorted)}
                          />
                        </button>
                      </th>
                    );
                  })}
                </tr>
              ))}
            </thead>
            <tbody>
              {padTop > 0 && (
                <tr aria-hidden="true">
                  <td style={{ height: padTop }} colSpan={columns.length} />
                </tr>
              )}
              {items.map((item) => {
                const row = tableRows[item.index];
                if (!row) return null;
                return (
                  <tr key={row.id}>
                    {row.getAllCells().map((cell) => (
                      <td key={cell.id}>
                        <table.FlexRender cell={cell} />
                      </td>
                    ))}
                  </tr>
                );
              })}
              {padBottom > 0 && (
                <tr aria-hidden="true">
                  <td style={{ height: padBottom }} colSpan={columns.length} />
                </tr>
              )}
            </tbody>
          </table>
          {hasNextPage && (
            <div className={styles.more}>
              {search ? (
                <span className={styles.muted}>Search covers the loaded {word} only.</span>
              ) : null}
              <Button size="sm" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
                {isFetchingNextPage ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function Cell({
  value,
  column,
  row,
  isName,
  resource,
}: {
  value: unknown;
  column: TableColumn;
  row: TableRow;
  isName: boolean;
  resource: ResourceRef;
}) {
  const text = formatCell(value);
  if (isName) {
    return (
      <Link to={detailPath(resource, row)} className={styles.name}>
        {text}
      </Link>
    );
  }
  if (isStatusColumn(column) && text !== "—") {
    return <Badge tone={statusTone(text)}>{text}</Badge>;
  }
  if (isNumeric(column)) return <span className={styles.num}>{text}</span>;
  if (isAgeColumn(column)) return <span className={styles.muted}>{text}</span>;
  return <span title={text.length > 40 ? text : undefined}>{text}</span>;
}

function ListError({
  error,
  word,
  where,
  selector,
  onRetry,
}: {
  error: Error;
  word: string;
  where: string;
  selector: string;
  onRetry: () => void;
}) {
  let message = error.message;
  if (error instanceof ApiError) {
    if (error.status === 403) message = `You can't list ${word}${where}. Ask an admin for access.`;
    else if (error.code === "unknown_resource") message = `This cluster doesn't have ${word}.`;
    else if (error.code === "bad_selector") message = error.message;
  }
  return (
    <div className={styles.error} role="alert">
      <p>{message}</p>
      {!(error instanceof ApiError && [400, 403, 404].includes(error.status)) && (
        <Button size="sm" onClick={onRetry}>
          Retry
        </Button>
      )}
      {selector && error instanceof ApiError && error.code === "bad_selector" && (
        <p className={styles.muted}>
          Selectors look like app=web, tier!=cache or env in (prod,staging).
        </p>
      )}
    </div>
  );
}
