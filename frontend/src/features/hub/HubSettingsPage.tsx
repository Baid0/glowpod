import { IconUserPlus } from "@tabler/icons-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type ReactNode } from "react";

import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Dialog } from "../../components/Dialog";
import { api, ApiError } from "../../lib/api";
import { Navigate } from "../../lib/clusterLinks";
import { useSession } from "../../lib/queries";
import { queryKeys } from "../../lib/queryKeys";
import type { CheckResult, HubUser, LDAPSettings, OIDCSettings } from "../../lib/types";
import access from "../access/Access.module.css";
import { ConfirmDialog } from "../access/SubjectDialog";
import { Command } from "../clusters/HubDialogs";
import styles from "./HubSettings.module.css";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

/** A Hub's own settings, for its administrators. */
export function HubSettingsPage() {
  const { data: session, isPending } = useSession();
  if (isPending) return null;
  if (session?.hub?.role !== "admin") return <Navigate to="/" replace />;
  return (
    <div className={access.page}>
      <ServerCard version={session.version ?? ""} />
      <AccountsCard me={session.user.username} />
      <SignInCards />
    </div>
  );
}

function ServerCard({ version }: { version: string }) {
  const client = useQueryClient();
  const { data } = useQuery({
    queryKey: queryKeys.hubSettings,
    queryFn: ({ signal }) => api.hubSettings(signal),
  });
  const [draft, setDraft] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const id = useId();
  const hint = useId();
  const value = draft ?? data?.serverURL ?? "";

  const save = async () => {
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      await api.setServerURL(value.trim());
      await client.invalidateQueries({ queryKey: queryKeys.hubSettings });
      setDraft(undefined);
      setSaved(true);
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title="Server">
      <form
        className={access.form}
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <div className={access.field}>
          <label htmlFor={id} className={access.label}>
            Server URL
          </label>
          <input
            id={id}
            type="url"
            className={access.input}
            aria-describedby={hint}
            required
            value={value}
            onChange={(e) => {
              setDraft(e.target.value);
              setSaved(false);
            }}
          />
          <p id={hint} className={access.hint}>
            The address people open and new clusters connect to. Clusters already imported keep the
            address they were given; choose Connect on each to move it.
          </p>
        </div>
        <div className={styles.buttons}>
          <Button type="submit" variant="primary" disabled={busy || draft === undefined}>
            {busy ? "Saving…" : "Save"}
          </Button>
          {saved && <span className={styles.result}>Saved.</span>}
          <span className={access.hint}>Glowpod {version}</span>
        </div>
        {error && (
          <p className={access.error} role="alert">
            {error}
          </p>
        )}
      </form>
    </Card>
  );
}

type AccountAction =
  { kind: "new" } | { kind: "password"; user: HubUser } | { kind: "delete"; user: HubUser };

function AccountsCard({ me }: { me: string }) {
  const client = useQueryClient();
  const { data, error } = useQuery({
    queryKey: queryKeys.hubUsers,
    queryFn: ({ signal }) => api.hubUsers(signal),
  });
  const [action, setAction] = useState<AccountAction | undefined>();
  const refresh = () => client.invalidateQueries({ queryKey: queryKeys.hubUsers });
  const close = () => {
    setAction(undefined);
  };
  const fixedRole = (u: HubUser) => u.username === "admin" || `glowpod:${u.username}` === me;

  return (
    <Card
      title="Glowpod accounts"
      actions={
        <Button
          size="sm"
          icon={<IconUserPlus size={16} stroke={1.75} />}
          onClick={() => {
            setAction({ kind: "new" });
          }}
        >
          Add account
        </Button>
      }
    >
      <p className={access.intro}>
        Accounts kept by the Hub, for people without SSO or LDAP. Kubernetes sees them as{" "}
        <span className={access.mono}>glowpod:&lt;username&gt;</span>; give them access under each
        cluster&apos;s Members.
      </p>
      {error ? (
        <p className={access.error} role="alert">
          {message(error)}
        </p>
      ) : (
        data && (
          <table className={access.table}>
            <thead>
              <tr>
                <th scope="col">Username</th>
                <th scope="col">Hub role</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {data.map((u) => (
                <tr key={u.username}>
                  <td className={access.mono}>{u.username}</td>
                  <td>{u.role === "admin" ? "Administrator" : "User"}</td>
                  <td>
                    <div className={access.actions}>
                      {!fixedRole(u) && (
                        <Button
                          size="sm"
                          onClick={() =>
                            void api
                              .updateHubUser(u.username, {
                                role: u.role === "admin" ? "user" : "admin",
                              })
                              .then(refresh)
                          }
                        >
                          {u.role === "admin" ? "Make user" : "Make administrator"}
                        </Button>
                      )}
                      <Button
                        size="sm"
                        aria-label={`Reset ${u.username}'s password`}
                        onClick={() => {
                          setAction({ kind: "password", user: u });
                        }}
                      >
                        Reset password
                      </Button>
                      {!fixedRole(u) && (
                        <Button
                          size="sm"
                          aria-label={`Delete ${u.username}`}
                          onClick={() => {
                            setAction({ kind: "delete", user: u });
                          }}
                        >
                          Delete
                        </Button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )
      )}
      {(action?.kind === "new" || action?.kind === "password") && (
        <AccountDialog
          user={action.kind === "password" ? action.user : undefined}
          onClose={close}
          save={async (username, role, password) => {
            if (action.kind === "password") await api.updateHubUser(username, { password });
            else await api.createHubUser(username, role, password);
            await refresh();
          }}
        />
      )}
      {action?.kind === "delete" && (
        <ConfirmDialog
          open
          onClose={close}
          title={`Delete ${action.user.username}?`}
          description="They're signed out at once, and their kubeconfigs stop working. Bindings that name them in clusters stay; remove those under each cluster's Members."
          action="Delete account"
          run={async () => {
            await api.deleteHubUser(action.user.username);
            await refresh();
          }}
        />
      )}
    </Card>
  );
}

/** A new account, or a new password for one. */
function AccountDialog({
  user,
  onClose,
  save,
}: {
  user?: HubUser;
  onClose: () => void;
  save: (username: string, role: HubUser["role"], password: string) => Promise<void>;
}) {
  const [username, setUsername] = useState(user?.username ?? "");
  const [role, setRole] = useState<HubUser["role"]>("user");
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const nameId = useId();
  const passId = useId();
  const againId = useId();

  const submit = async () => {
    if (password !== again) {
      setError("The two passwords don't match.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await save(username.trim(), role, password);
      onClose();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open
      onClose={onClose}
      title={user ? `New password for ${user.username}` : "Add a Glowpod account"}
      description={
        user ? (
          <p>They&apos;re signed out everywhere, and sign in again with this one.</p>
        ) : undefined
      }
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? "Saving…" : user ? "Set password" : "Add account"}
          </Button>
        </>
      }
    >
      <div className={access.form}>
        {!user && (
          <>
            <div className={access.field}>
              <label htmlFor={nameId} className={access.label}>
                Username
              </label>
              <input
                id={nameId}
                className={access.input}
                autoComplete="off"
                spellCheck={false}
                required
                value={username}
                onChange={(e) => {
                  setUsername(e.target.value);
                }}
              />
            </div>
            <fieldset className={access.roles}>
              <legend className={access.label}>Hub role</legend>
              <label className={access.role}>
                <input
                  type="radio"
                  name="hub-role"
                  checked={role === "user"}
                  onChange={() => {
                    setRole("user");
                  }}
                />
                <span>
                  <span className={access.roleName}>User</span> · sees the clusters they&apos;re
                  given access to
                </span>
              </label>
              <label className={access.role}>
                <input
                  type="radio"
                  name="hub-role"
                  checked={role === "admin"}
                  onChange={() => {
                    setRole("admin");
                  }}
                />
                <span>
                  <span className={access.roleName}>Administrator</span> · manages accounts, sign-in
                  and clusters, with no rights inside clusters by that alone
                </span>
              </label>
            </fieldset>
          </>
        )}
        <div className={access.field}>
          <label htmlFor={passId} className={access.label}>
            Password
          </label>
          <input
            id={passId}
            type="password"
            className={access.input}
            autoComplete="new-password"
            minLength={12}
            required
            value={password}
            onChange={(e) => {
              setPassword(e.target.value);
            }}
          />
          <p className={access.hint}>At least 12 characters. Give it to them in person.</p>
        </div>
        <div className={access.field}>
          <label htmlFor={againId} className={access.label}>
            The same password again
          </label>
          <input
            id={againId}
            type="password"
            className={access.input}
            autoComplete="new-password"
            required
            value={again}
            onChange={(e) => {
              setAgain(e.target.value);
            }}
          />
        </div>
        {error && (
          <p className={access.error} role="alert">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}

function SignInCards() {
  const { data, error } = useQuery({
    queryKey: queryKeys.hubSettings,
    queryFn: ({ signal }) => api.hubSettings(signal),
  });
  if (error) {
    return (
      <p className={access.error} role="alert">
        {message(error)}
      </p>
    );
  }
  if (!data) return null;
  return (
    <>
      <OIDCCard initial={data.auth.oidc} fixed={data.fixed.oidc} redirectURL={data.redirectURL} />
      <LDAPCard initial={data.auth.ldap} fixed={data.fixed.ldap} />
    </>
  );
}

/** Save and Check for one sign-in method's form. */
function useMethodForm<T>(save: (v: T) => Promise<void>, check: (v: T) => Promise<CheckResult>) {
  const client = useQueryClient();
  const [busy, setBusy] = useState<"" | "save" | "check">("");
  const [result, setResult] = useState<{ ok: boolean; text: string } | undefined>();
  /** Resolves true when it worked. */
  const run = async (what: "save" | "check", v: T) => {
    setBusy(what);
    setResult(undefined);
    try {
      if (what === "check") {
        const r = await check(v);
        setResult({ ok: r.ok, text: r.message });
        return r.ok;
      }
      await save(v);
      await client.invalidateQueries({ queryKey: queryKeys.hubSettings });
      setResult({ ok: true, text: "Saved. The sign-in page shows it now." });
      return true;
    } catch (err) {
      setResult({ ok: false, text: message(err) });
      return false;
    } finally {
      setBusy("");
    }
  };
  return { busy, result, run };
}

function Field({
  label,
  hint,
  wide = false,
  children,
}: {
  label: string;
  hint?: string;
  wide?: boolean;
  children: (id: string) => ReactNode;
}) {
  const id = useId();
  return (
    <div className={`${access.field} ${wide ? styles.wide : ""}`}>
      <label htmlFor={id} className={access.label}>
        {label}
      </label>
      {children(id)}
      {hint && <p className={access.hint}>{hint}</p>}
    </div>
  );
}

function Result({ result }: { result?: { ok: boolean; text: string } }) {
  if (!result) return null;
  return (
    <p className={styles.result} role="status">
      <Badge tone={result.ok ? "ok" : "critical"}>{result.ok ? "OK" : "Failed"}</Badge>
      {result.text}
    </p>
  );
}

function OIDCCard({
  initial,
  fixed,
  redirectURL,
}: {
  initial: OIDCSettings;
  fixed: boolean;
  redirectURL: string;
}) {
  const [v, setV] = useState<OIDCSettings>({ ...initial, clientSecret: "" });
  // Kept as typed; split into scopes only when sent.
  const [scopes, setScopes] = useState(initial.scopes.join(", "));
  const withScopes = (): OIDCSettings => ({
    ...v,
    scopes: scopes
      .split(",")
      .map((x) => x.trim())
      .filter(Boolean),
  });
  const set = (patch: Partial<OIDCSettings>) => {
    setV((cur) => ({ ...cur, ...patch }));
  };
  const form = useMethodForm(api.setOIDC, api.checkOIDC);
  const text = (k: "name" | "issuer" | "clientId" | "usernameClaim" | "groupsClaim") => ({
    value: v[k],
    onChange: (e: { target: { value: string } }) => {
      set({ [k]: e.target.value });
    },
  });

  return (
    <Card title="Single sign-on (OIDC)">
      {fixed ? (
        <p className={styles.note}>
          SSO is set with Helm values (<span className={access.mono}>auth.oidc</span>) or flags;
          change it there.
        </p>
      ) : (
        <form
          className={access.form}
          onSubmit={(e) => {
            e.preventDefault();
            void form.run("save", withScopes()).then((ok) => {
              if (ok) set({ clientSecret: "" });
            });
          }}
        >
          <label className={styles.toggle}>
            <input
              type="checkbox"
              checked={v.enabled}
              onChange={(e) => {
                set({ enabled: e.target.checked });
              }}
            />
            Sign in with SSO
          </label>
          <div className={styles.grid}>
            <Field label="Button label" hint="On the sign-in page: Sign in with …">
              {(id) => <input id={id} className={access.input} {...text("name")} />}
            </Field>
            <Field
              label="Issuer URL"
              hint="Such as https://login.microsoftonline.com/<tenant>/v2.0"
            >
              {(id) => (
                <input
                  id={id}
                  type="url"
                  className={access.input}
                  spellCheck={false}
                  {...text("issuer")}
                />
              )}
            </Field>
            <Field label="Client ID">
              {(id) => (
                <input id={id} className={access.input} spellCheck={false} {...text("clientId")} />
              )}
            </Field>
            <Field
              label="Client secret"
              hint={initial.clientSecretSet ? "Kept. Leave empty to keep it." : undefined}
            >
              {(id) => (
                <input
                  id={id}
                  type="password"
                  className={access.input}
                  autoComplete="off"
                  value={v.clientSecret ?? ""}
                  onChange={(e) => {
                    set({ clientSecret: e.target.value });
                  }}
                />
              )}
            </Field>
            <Field label="Scopes" hint="Comma-separated; add groups if your provider needs it.">
              {(id) => (
                <input
                  id={id}
                  className={access.input}
                  spellCheck={false}
                  value={scopes}
                  onChange={(e) => {
                    setScopes(e.target.value);
                  }}
                />
              )}
            </Field>
            <Field label="Username claim" hint="Kubernetes sees oidc:<this claim>.">
              {(id) => (
                <input
                  id={id}
                  className={access.input}
                  spellCheck={false}
                  {...text("usernameClaim")}
                />
              )}
            </Field>
            <Field label="Groups claim" hint="Groups become oidc:<group>.">
              {(id) => (
                <input
                  id={id}
                  className={access.input}
                  spellCheck={false}
                  {...text("groupsClaim")}
                />
              )}
            </Field>
          </div>
          <div className={access.field}>
            <span className={access.label}>Redirect URL to register with the provider</span>
            <Command text={redirectURL} label="Redirect URL" />
          </div>
          <div className={styles.buttons}>
            <Button
              disabled={form.busy !== ""}
              onClick={() => void form.run("check", withScopes())}
            >
              {form.busy === "check" ? "Checking…" : "Check"}
            </Button>
            <Button type="submit" variant="primary" disabled={form.busy !== ""}>
              {form.busy === "save" ? "Saving…" : "Save"}
            </Button>
          </div>
          <Result result={form.result} />
        </form>
      )}
    </Card>
  );
}

function LDAPCard({ initial, fixed }: { initial: LDAPSettings; fixed: boolean }) {
  const [v, setV] = useState<LDAPSettings>({ ...initial, bindPassword: "" });
  const set = (patch: Partial<LDAPSettings>) => {
    setV((cur) => ({ ...cur, ...patch }));
  };
  const form = useMethodForm(api.setLDAP, api.checkLDAP);
  type TextKey =
    | "url"
    | "bindDN"
    | "userBase"
    | "userFilter"
    | "usernameAttribute"
    | "groupBase"
    | "groupFilter"
    | "groupAttribute";
  const text = (k: TextKey) => ({
    value: v[k],
    spellCheck: false,
    onChange: (e: { target: { value: string } }) => {
      set({ [k]: e.target.value });
    },
  });

  return (
    <Card title="LDAP / Active Directory">
      {fixed ? (
        <p className={styles.note}>
          LDAP is set with Helm values (<span className={access.mono}>auth.ldap</span>) or flags;
          change it there.
        </p>
      ) : (
        <form
          className={access.form}
          onSubmit={(e) => {
            e.preventDefault();
            void form.run("save", v).then((ok) => {
              if (ok) set({ bindPassword: "" });
            });
          }}
        >
          <label className={styles.toggle}>
            <input
              type="checkbox"
              checked={v.enabled}
              onChange={(e) => {
                set({ enabled: e.target.checked });
              }}
            />
            Sign in with LDAP
          </label>
          <div className={styles.grid}>
            <Field label="Server URL" hint="ldaps://ldap.example.com, or ldap:// with StartTLS.">
              {(id) => <input id={id} className={access.input} {...text("url")} />}
            </Field>
            <Field label="Bind DN" hint="The account Glowpod searches with; empty for anonymous.">
              {(id) => <input id={id} className={access.input} {...text("bindDN")} />}
            </Field>
            <Field
              label="Bind password"
              hint={initial.bindPasswordSet ? "Kept. Leave empty to keep it." : undefined}
            >
              {(id) => (
                <input
                  id={id}
                  type="password"
                  className={access.input}
                  autoComplete="off"
                  value={v.bindPassword ?? ""}
                  onChange={(e) => {
                    set({ bindPassword: e.target.value });
                  }}
                />
              )}
            </Field>
            <Field label="User base" hint="Such as ou=people,dc=example,dc=com">
              {(id) => <input id={id} className={access.input} {...text("userBase")} />}
            </Field>
            <Field label="User filter" hint="{username} is what people type, escaped.">
              {(id) => <input id={id} className={access.input} {...text("userFilter")} />}
            </Field>
            <Field label="Username attribute" hint="Kubernetes sees ldap:<this attribute>.">
              {(id) => <input id={id} className={access.input} {...text("usernameAttribute")} />}
            </Field>
            <Field label="Group base" hint="Empty: no groups.">
              {(id) => <input id={id} className={access.input} {...text("groupBase")} />}
            </Field>
            <Field label="Group filter" hint="{dn} and {username} are escaped.">
              {(id) => <input id={id} className={access.input} {...text("groupFilter")} />}
            </Field>
            <Field label="Group attribute" hint="Groups become ldap:<this attribute>.">
              {(id) => <input id={id} className={access.input} {...text("groupAttribute")} />}
            </Field>
            <Field
              label="CA certificate"
              hint="PEM, for a server whose certificate a private CA signed."
              wide
            >
              {(id) => (
                <textarea
                  id={id}
                  className={styles.textarea}
                  spellCheck={false}
                  value={v.ca}
                  onChange={(e) => {
                    set({ ca: e.target.value });
                  }}
                />
              )}
            </Field>
          </div>
          <label className={styles.toggle}>
            <input
              type="checkbox"
              checked={v.startTLS}
              onChange={(e) => {
                set({ startTLS: e.target.checked });
              }}
            />
            Use StartTLS (for ldap://)
          </label>
          <div className={styles.buttons}>
            <Button disabled={form.busy !== ""} onClick={() => void form.run("check", v)}>
              {form.busy === "check" ? "Checking…" : "Check"}
            </Button>
            <Button type="submit" variant="primary" disabled={form.busy !== ""}>
              {form.busy === "save" ? "Saving…" : "Save"}
            </Button>
          </div>
          <Result result={form.result} />
        </form>
      )}
    </Card>
  );
}
