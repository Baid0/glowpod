# Decisions

Choices made while building Glowpod that aren't obvious from CLAUDE.md, or
that depart from it. Newest last.

## YAML editor: CodeMirror 6 instead of Monaco (Phase 3)

CLAUDE.md lists Monaco. Glowpod uses CodeMirror 6 instead because:

- **CSP.** Monaco needs web workers (`worker-src blob:`) and injects inline
  styles in many places. CodeMirror creates one `<style>` element, which
  takes a nonce, so the strict policy stays: `script-src 'self'`, and
  `style-src 'self' 'nonce-…'` with a fresh nonce on every page load.
- **Size.** About 370 KB, loaded only when someone edits, against several MB
  for Monaco.
- **Accessibility.** CodeMirror's content is a real `contenteditable` with
  an `aria-label`. Esc then Tab leaves the editor.

To switch back, replace `frontend/src/features/yaml/CodeEditor.tsx`; the edit
flow around it (`EditYaml.tsx`) doesn't depend on the editor.

## Edits are full updates with a dry run first (Phase 3)

Saving YAML sends `PUT` (update) with the `resourceVersion` the editor
started from, after a server-side dry run that returns before/after for the
diff. A change someone else made in between is a conflict (409), never
silently overwritten. Name, namespace, kind and apiVersion can't change in
an edit. A Secret can only be edited after its values are revealed, and the
server refuses a Secret sent back with every value blank.

## CSRF (Phase 3)

Every write needs the session's CSRF token in `X-CSRF-Token` (from
`GET /api/v1/session`) and must not come from another origin
(`Origin` / `Sec-Fetch-Site`). This is on top of the `SameSite=Strict`
session cookie.

## Hiding actions (Phase 3)

`POST /api/v1/clusters/{cluster}/access` runs one `SelfSubjectAccessReview`
per check. The UI shows a button only when discovery says the API supports
the verb **and** the review allows it. A 403 on the real request is still
shown in plain words, with the cluster's reason.
