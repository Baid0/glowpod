import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { yaml } from "@codemirror/lang-yaml";
import { HighlightStyle, indentUnit, syntaxHighlighting } from "@codemirror/language";
import { unifiedMergeView } from "@codemirror/merge";
import { highlightSelectionMatches, search, searchKeymap } from "@codemirror/search";
import { EditorState, type Extension } from "@codemirror/state";
import {
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";
import { useEffect, useRef } from "react";

import { cspNonce } from "../../lib/csp";
import styles from "./CodeEditor.module.css";

// Same palette as the read-only view: neutral greys, keys in ink.
const highlight = HighlightStyle.define([
  { tag: tags.definition(tags.propertyName), color: "var(--ink)", fontWeight: "500" },
  { tag: [tags.string, tags.content, tags.special(tags.string)], color: "var(--body)" },
  { tag: [tags.lineComment, tags.meta], color: "var(--muted)", fontStyle: "italic" },
  {
    tag: [tags.separator, tags.punctuation, tags.squareBracket, tags.brace],
    color: "var(--muted)",
  },
  { tag: [tags.labelName, tags.typeName, tags.keyword], color: "var(--primary)" },
]);

const theme = EditorView.theme(
  {
    "&": {
      height: "100%",
      backgroundColor: "var(--canvas)",
      color: "var(--body)",
      fontSize: "13px",
    },
    ".cm-scroller": { fontFamily: "var(--font-mono)", lineHeight: "20px" },
    ".cm-content": { padding: "12px 0", caretColor: "var(--ink)" },
    ".cm-cursor": { borderLeftColor: "var(--ink)" },
    "&.cm-focused": { outline: "2px solid var(--focus-ring)", outlineOffset: "-2px" },
    ".cm-gutters": {
      backgroundColor: "var(--canvas)",
      color: "var(--muted-soft)",
      border: "none",
    },
    ".cm-lineNumbers .cm-gutterElement": { minWidth: "40px", padding: "0 12px 0 8px" },
    ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "var(--surface-card)" },
    ".cm-activeLineGutter": { color: "var(--body)" },
    "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": {
      backgroundColor: "var(--surface-elevated) !important",
    },
    ".cm-selectionMatch": { backgroundColor: "var(--surface-elevated)" },
    ".cm-panels": {
      backgroundColor: "var(--surface-card)",
      color: "var(--body)",
      borderColor: "var(--hairline)",
    },
    ".cm-panels input, .cm-panels button": { font: "inherit", color: "var(--ink)" },
    ".cm-textfield": {
      backgroundColor: "var(--canvas)",
      border: "1px solid var(--hairline-strong)",
      borderRadius: "6px",
    },
    ".cm-button": {
      backgroundImage: "none",
      backgroundColor: "var(--surface-elevated)",
      border: "none",
      borderRadius: "6px",
    },
    // Diff: lines the change adds or removes. Neutral fills and a bar at the
    // edge, so the diff never relies on color alone.
    ".cm-changedLine": {
      backgroundColor: "var(--surface-card)",
      borderLeft: "3px solid var(--primary)",
    },
    ".cm-deletedChunk": {
      backgroundColor: "var(--surface-elevated)",
      borderLeft: "3px solid var(--muted)",
    },
    // The merge view's default red and green would break "rose = critical only".
    ".cm-deletedChunk del, .cm-deletedText": {
      color: "var(--muted)",
      textDecoration: "line-through",
      background: "none",
    },
    ".cm-insertedLine, .cm-changedText": {
      background: "none",
      color: "var(--ink)",
      textDecoration: "underline",
      textDecorationColor: "var(--primary)",
      textUnderlineOffset: "3px",
    },
    ".cm-collapsedLines": {
      backgroundColor: "var(--surface-card)",
      backgroundImage: "none",
      color: "var(--muted)",
      fontFamily: "var(--font-sans)",
      fontSize: "12px",
    },
  },
  { dark: true },
);

function base(label: string): Extension[] {
  const nonce = cspNonce();
  return [
    nonce ? EditorView.cspNonce.of(nonce) : [],
    lineNumbers(),
    highlightActiveLineGutter(),
    drawSelection(),
    yaml(),
    indentUnit.of("  "),
    EditorState.tabSize.of(2),
    syntaxHighlighting(highlight),
    theme,
    EditorView.contentAttributes.of({ "aria-label": label }),
  ];
}

function useView(make: (parent: HTMLElement) => EditorView, deps: unknown[]) {
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!host.current) return;
    const view = make(host.current);
    return () => {
      view.destroy();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the view is rebuilt only when deps change
  }, deps);
  return host;
}

/**
 * A YAML editor. Tab indents; press Esc then Tab to move focus on.
 * The initial text is read once: change `key` to load new text.
 */
export function YamlEditor({
  initial,
  label,
  onChange,
}: {
  initial: string;
  label: string;
  onChange: (text: string) => void;
}) {
  const changed = useRef(onChange);
  useEffect(() => {
    changed.current = onChange;
  });
  const host = useView(
    (parent) =>
      new EditorView({
        parent,
        state: EditorState.create({
          doc: initial,
          extensions: [
            ...base(label),
            history(),
            highlightActiveLine(),
            highlightSelectionMatches(),
            search({ top: true }),
            keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab]),
            EditorView.updateListener.of((u) => {
              if (u.docChanged) changed.current(u.state.doc.toString());
            }),
          ],
        }),
      }),
    [],
  );
  return <div ref={host} className={styles.editor} />;
}

/** A read-only, unified diff: before → after, unchanged runs folded away. */
export function YamlDiff({
  before,
  after,
  label,
}: {
  before: string;
  after: string;
  label: string;
}) {
  const host = useView(
    (parent) =>
      new EditorView({
        parent,
        state: EditorState.create({
          doc: after,
          extensions: [
            ...base(label),
            EditorState.readOnly.of(true),
            EditorView.editable.of(false),
            unifiedMergeView({
              original: before,
              mergeControls: false,
              gutter: true,
              collapseUnchanged: { margin: 3, minSize: 6 },
            }),
          ],
        }),
      }),
    [before, after],
  );
  return <div ref={host} className={styles.editor} />;
}
