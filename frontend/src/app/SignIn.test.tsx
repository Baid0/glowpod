import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import * as fx from "../test/fixtures";
import { renderApp } from "../test/renderApp";
import { safeReturn } from "./returnPath";

const serverSession = {
  ...fx.session,
  mode: "in-cluster" as const,
  method: "ldap" as const,
  user: { username: "alice", groups: ["devs", "oncall"] },
};

interface Posted {
  path: string;
  body: unknown;
  csrf: string | null;
}

/** A server-mode backend: signed out until a sign-in succeeds. */
function serverMode(
  methods: { type: string; name?: string }[],
  opts: { restricted?: boolean } = {},
) {
  let signedIn = false;
  const posted: Posted[] = [];
  const app = renderApp("/sign-in?return=%2Fworkloads%2Fpods", (url, init) => {
    if (init?.method === "POST") {
      posted.push({
        path: url.pathname,
        body: typeof init.body === "string" ? JSON.parse(init.body) : undefined,
        csrf: new Headers(init.headers).get("X-CSRF-Token"),
      });
    }
    switch (url.pathname) {
      case "/api/v1/auth/methods":
        return Response.json({ methods });
      case "/api/v1/auth/ldap": {
        const body = JSON.parse(init?.body as string) as { password: string };
        if (body.password !== "correct horse") {
          return Response.json(
            {
              error: {
                code: "bad_credentials",
                message: "That username and password didn't work.",
              },
            },
            { status: 401 },
          );
        }
        signedIn = true;
        return Response.json({});
      }
      case "/api/v1/auth/token":
        signedIn = true;
        return Response.json({});
      case "/api/v1/auth/logout":
        signedIn = false;
        return new Response(null, { status: 204 });
      case "/api/v1/session":
        return signedIn
          ? Response.json(serverSession)
          : Response.json(
              { error: { code: "unauthenticated", message: "Sign in." } },
              { status: 401 },
            );
      case "/api/v1/clusters/default/namespaces":
        return Response.json(
          opts.restricted
            ? { namespaces: ["team-a"], restricted: true }
            : { namespaces: fx.namespaces },
        );
      default:
        return undefined;
    }
  });
  return { ...app, posted };
}

describe("sign-in page", () => {
  it("offers SSO first, then LDAP and a token", async () => {
    serverMode([{ type: "oidc", name: "Okta" }, { type: "ldap", name: "LDAP" }, { type: "token" }]);
    const sso = await screen.findByRole("link", { name: "Sign in with Okta" });
    expect(sso).toHaveAttribute("href", "/auth/oidc/login?return=%2Fworkloads%2Fpods");
    expect(
      screen.getByRole("form", { name: "Sign in with your directory account" }),
    ).toBeInTheDocument();
    // The token form opens on request when other methods exist.
    await userEvent.click(screen.getByRole("button", { name: "Use a service account token" }));
    expect(screen.getByLabelText("Service account token")).toBeInTheDocument();
  });

  it("signs in with LDAP, then goes where the user was headed", async () => {
    const { router, posted } = serverMode([{ type: "ldap" }]);
    const form = await screen.findByRole("form", { name: "Sign in with your directory account" });
    await userEvent.type(within(form).getByLabelText("Username"), "alice");
    await userEvent.type(within(form).getByLabelText("Password"), "wrong");
    await userEvent.click(within(form).getByRole("button", { name: "Sign in" }));
    expect(await within(form).findByRole("alert")).toHaveTextContent("didn't work");

    await userEvent.clear(within(form).getByLabelText("Password"));
    await userEvent.type(within(form).getByLabelText("Password"), "correct horse");
    await userEvent.click(within(form).getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("button", { name: "Signed in as alice" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/c/default/workloads/pods");
    expect(posted[0]).toMatchObject({
      path: "/api/v1/auth/ldap",
      body: { username: "alice" },
      csrf: null,
    });
  });

  it("signs in with a token when that's the only way", async () => {
    const { posted } = serverMode([{ type: "token" }]);
    const field = await screen.findByLabelText("Service account token");
    await userEvent.type(field, "eyJhbGci.token");
    await userEvent.click(screen.getByRole("button", { name: "Sign in with token" }));
    expect(await screen.findByRole("button", { name: "Signed in as alice" })).toBeInTheDocument();
    expect(posted[0]).toMatchObject({
      path: "/api/v1/auth/token",
      body: { token: "eyJhbGci.token" },
    });
  });

  it("explains SSO errors by code, never echoing text", async () => {
    renderApp("/sign-in?error=sso_no_user", (url) =>
      url.pathname === "/api/v1/auth/methods"
        ? Response.json({ methods: [{ type: "oidc" }] })
        : undefined,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("verified email");
  });

  it("ignores unknown error codes", async () => {
    renderApp("/sign-in?error=<script>alert(1)</script>", (url) =>
      url.pathname === "/api/v1/auth/methods"
        ? Response.json({ methods: [{ type: "oidc" }] })
        : undefined,
    );
    await screen.findByRole("link", { name: "Sign in with SSO" });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("user menu", () => {
  it("shows who you are and signs out with the CSRF token", async () => {
    const { router, posted } = serverMode([{ type: "token" }]);
    await userEvent.type(await screen.findByLabelText("Service account token"), "t");
    await userEvent.click(screen.getByRole("button", { name: "Sign in with token" }));

    await userEvent.click(await screen.findByRole("button", { name: "Signed in as alice" }));
    const menu = screen.getByRole("region", { name: "Account" });
    expect(menu).toHaveTextContent("Signed in with your directory account");
    expect(within(menu).getByRole("list", { name: "Groups" })).toHaveTextContent("devs");

    await userEvent.click(within(menu).getByRole("button", { name: "Sign out" }));
    expect(await screen.findByRole("heading", { name: "Sign in to Glowpod" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/sign-in");
    expect(posted.at(-1)).toMatchObject({
      path: "/api/v1/auth/logout",
      csrf: fx.session.csrfToken,
    });
  });

  it("has no sign-out in local mode", async () => {
    renderApp("/");
    await userEvent.click(
      await screen.findByRole("button", { name: "Signed in as kubernetes-admin" }),
    );
    expect(screen.getByRole("region", { name: "Account" })).toHaveTextContent(
      "Using your kubeconfig",
    );
    expect(screen.queryByRole("button", { name: "Sign out" })).not.toBeInTheDocument();
  });
});

describe("namespace picker for people who can't list namespaces", () => {
  it("starts them in one they can use and lets them type another", async () => {
    const { posted } = serverMode([{ type: "token" }], { restricted: true });
    await userEvent.type(await screen.findByLabelText("Service account token"), "t");
    await userEvent.click(screen.getByRole("button", { name: "Sign in with token" }));

    const picker = () => screen.getByRole("button", { name: /^Namespace:/ });
    // The list arrives, then the picker moves to the namespace they can use.
    await waitFor(() => {
      expect(picker()).toHaveAccessibleName("Namespace: team-a");
    });

    await userEvent.click(picker());
    expect(screen.queryByRole("option", { name: "All namespaces" })).not.toBeInTheDocument();
    await userEvent.type(screen.getByRole("combobox", { name: "Search namespaces" }), "team-b");
    await userEvent.click(screen.getByRole("option", { name: "Use team-b" }));
    expect(picker()).toHaveAccessibleName("Namespace: team-b");
    expect(posted.length).toBeGreaterThan(0);
  });
});

describe("safeReturn", () => {
  it.each([
    ["/workloads", "/workloads"],
    [null, "/"],
    ["https://evil.example", "/"],
    ["//evil.example", "/"],
    ["/sign-in", "/"],
  ])("%s → %s", (input, want) => {
    expect(safeReturn(input)).toBe(want);
  });
});

describe("sign-in with LDAP and Glowpod accounts", () => {
  it("titles each username form, so they can't be mixed up", async () => {
    const user = userEvent.setup();
    serverMode([{ type: "oidc", name: "Dex" }, { type: "ldap" }, { type: "password" }]);
    const ldap = await screen.findByRole("form", { name: "Sign in with your directory account" });
    expect(within(ldap).getByText("Directory account")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Sign in with a Glowpod account" }));
    const account = await screen.findByRole("form", { name: "Sign in with a Glowpod account" });
    expect(within(account).getByText("Glowpod account")).toBeInTheDocument();
    expect(within(account).queryByText("Directory account")).not.toBeInTheDocument();
  });
});
