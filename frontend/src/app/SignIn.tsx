import { IconKey, IconLogin2, IconUser } from "@tabler/icons-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useState } from "react";
import { useSearchParams } from "react-router";

import { Button } from "../components/Button";
import { api, ApiError } from "../lib/api";
import type { AuthMethod } from "../lib/types";
import { Logo } from "./Logo";
import { safeReturn } from "./returnPath";
import styles from "./SignIn.module.css";
import { useNavigate } from "../lib/clusterPaths";

/** Messages for ?error= codes from the SSO redirect; nothing else is shown. */
const ERRORS: Record<string, string> = {
  sso_refused: "Sign-in was cancelled or refused by your provider.",
  sso_expired: "That sign-in took too long, or was finished in another browser. Try again.",
  sso_failed: "Couldn't finish signing in with your provider. Try again.",
  sso_unavailable: "Your sign-in provider can't be reached right now. Try again in a moment.",
  sso_no_user:
    "Your account didn't come with a verified email, so Glowpod can't tell who you are. Ask an admin.",
  too_many_attempts: "Too many sign-in attempts. Wait a few minutes and try again.",
  cluster_rejected: "The cluster stopped accepting your sign-in. Sign in again.",
};

/** Sign in with SSO, LDAP, a Hub account or a token, whichever the install allows. */
export function SignInPage() {
  useEffect(() => {
    document.title = "Sign in · Glowpod";
  }, []);
  const [params] = useSearchParams();
  const returnTo = safeReturn(params.get("return"));
  const error = params.get("error");
  // After a restore: how many accounts and clusters came back, as "2-1".
  const restored = /^(\d+)-(\d+)$/.exec(params.get("restored") ?? "");
  const {
    data: methods,
    error: methodsError,
    isPending,
  } = useQuery({
    queryKey: ["authMethods"],
    queryFn: ({ signal }) => api.authMethods(signal),
    retry: false,
    staleTime: Infinity,
  });
  // Local mode has no sign-in page: the launch link signs you in.
  const local = methodsError instanceof ApiError && methodsError.status === 404;

  return (
    <main className={styles.page}>
      <div className={styles.card}>
        <Logo size={40} />
        <h1 className={styles.title}>Sign in to Glowpod</h1>
        {error && ERRORS[error] && (
          <p className={styles.error} role="alert">
            {ERRORS[error]}
          </p>
        )}
        {restored && (
          <p className={styles.text} role="status">
            The backup is restored: {restored[1]} Glowpod accounts and {restored[2]} clusters. Sign
            in with an account from it.
          </p>
        )}
        {isPending ? (
          <div className={styles.skeleton} aria-label="Loading sign-in options" />
        ) : local ? (
          <p className={styles.text}>
            Open Glowpod from the link printed where you ran <code>glowpod</code>. It starts with{" "}
            <code>http://127.0.0.1</code>.
          </p>
        ) : methodsError ? (
          <p className={styles.error} role="alert">
            {methodsError.message}
          </p>
        ) : (
          <Methods methods={methods} returnTo={returnTo} />
        )}
      </div>
    </main>
  );
}

function Methods({ methods, returnTo }: { methods: AuthMethod[]; returnTo: string }) {
  const oidc = methods.find((m) => m.type === "oidc");
  const ldap = methods.some((m) => m.type === "ldap");
  const password = methods.some((m) => m.type === "password");
  const token = methods.some((m) => m.type === "token");
  return (
    <div className={styles.methods}>
      {oidc && (
        <a className={styles.sso} href={`/auth/oidc/login?return=${encodeURIComponent(returnTo)}`}>
          <IconLogin2 size={18} stroke={1.75} aria-hidden="true" />
          Sign in with {oidc.name ?? "SSO"}
        </a>
      )}
      {oidc && (ldap || password || token) && <Divider />}
      {ldap && (
        <PasswordForm
          returnTo={returnTo}
          primary={!oidc}
          label="Sign in with your directory account"
          title="Directory account"
          hint="Your company username and password (LDAP)."
          signIn={api.signInLDAP}
        />
      )}
      {ldap && (password || token) && <Divider />}
      {password && <AccountForm returnTo={returnTo} open={!oidc && !ldap} />}
      {password && token && <Divider />}
      {token && <TokenForm returnTo={returnTo} open={!oidc && !ldap && !password} />}
    </div>
  );
}

/** A Hub's local accounts; admin's first password is the bootstrap one. */
function AccountForm({ returnTo, open }: { returnTo: string; open: boolean }) {
  const [shown, setShown] = useState(open);
  if (!shown) {
    return (
      <Button
        icon={<IconUser size={16} stroke={1.75} />}
        onClick={() => {
          setShown(true);
        }}
      >
        Sign in with a Glowpod account
      </Button>
    );
  }
  return (
    <>
      <PasswordForm
        returnTo={returnTo}
        primary={open}
        label="Sign in with a Glowpod account"
        title="Glowpod account"
        hint="An account made on this Hub, such as admin."
        signIn={api.signInPassword}
      />
      <p className={styles.hint}>
        First time? Sign in as <code>admin</code> with the bootstrap password. The Hub's log says
        how to read it.
      </p>
    </>
  );
}

function Divider() {
  return (
    <p className={styles.divider} aria-hidden="true">
      <span>or</span>
    </p>
  );
}

/** Shared by the forms: sign in, drop anything cached, go where they were headed. */
function useSignIn(returnTo: string) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const run = async (signIn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await signIn();
      // Nothing from before (another user, a signed-out state) may linger.
      client.clear();
      await navigate(returnTo, { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong.");
      setBusy(false);
    }
  };
  return { busy, error, run };
}

function PasswordForm({
  returnTo,
  primary,
  label,
  title,
  hint,
  signIn,
}: {
  returnTo: string;
  primary: boolean;
  label: string;
  /** Shown above the fields, so two username forms can't be mixed up. */
  title: string;
  hint: string;
  signIn: (username: string, password: string) => Promise<void>;
}) {
  const { busy, error, run } = useSignIn(returnTo);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const userId = useId();
  const passId = useId();
  return (
    <form
      className={styles.form}
      onSubmit={(e) => {
        e.preventDefault();
        void run(() => signIn(username, password));
      }}
      aria-label={label}
    >
      <div className={styles.formHead}>
        <p className={styles.formTitle}>{title}</p>
        <p className={styles.formHint}>{hint}</p>
      </div>
      <label htmlFor={userId} className={styles.label}>
        Username
      </label>
      <input
        id={userId}
        className={styles.input}
        autoComplete="username"
        required
        value={username}
        onChange={(e) => {
          setUsername(e.target.value);
        }}
      />
      <label htmlFor={passId} className={styles.label}>
        Password
      </label>
      <input
        id={passId}
        type="password"
        className={styles.input}
        autoComplete="current-password"
        required
        value={password}
        onChange={(e) => {
          setPassword(e.target.value);
        }}
      />
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      <Button type="submit" variant={primary ? "primary" : "secondary"} disabled={busy}>
        {busy ? "Signing in…" : "Sign in"}
      </Button>
    </form>
  );
}

function TokenForm({ returnTo, open }: { returnTo: string; open: boolean }) {
  const { busy, error, run } = useSignIn(returnTo);
  const [shown, setShown] = useState(open);
  const [token, setToken] = useState("");
  const tokenId = useId();
  const hintId = useId();
  if (!shown) {
    return (
      <Button
        icon={<IconKey size={16} stroke={1.75} />}
        onClick={() => {
          setShown(true);
        }}
      >
        Use a service account token
      </Button>
    );
  }
  return (
    <form
      className={styles.form}
      aria-label="Sign in with a token"
      onSubmit={(e) => {
        e.preventDefault();
        void run(() => api.signInToken(token));
      }}
    >
      <label htmlFor={tokenId} className={styles.label}>
        Service account token
      </label>
      <textarea
        id={tokenId}
        className={styles.token}
        aria-describedby={hintId}
        autoComplete="off"
        spellCheck={false}
        required
        rows={4}
        value={token}
        onChange={(e) => {
          setToken(e.target.value);
        }}
      />
      <p id={hintId} className={styles.hint}>
        Create one with <code>kubectl create token NAME -n NAMESPACE</code>. Glowpod keeps it on the
        server for this session only and acts with exactly its rights.
      </p>
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      <Button type="submit" variant={open ? "primary" : "secondary"} disabled={busy}>
        {busy ? "Checking…" : "Sign in with token"}
      </Button>
    </form>
  );
}
