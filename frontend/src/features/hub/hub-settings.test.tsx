import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { HubSettings, Session } from "../../lib/types";
import * as fx from "../../test/fixtures";
import { renderApp } from "../../test/renderApp";

const admin: Session = {
  ...fx.session,
  mode: "in-cluster",
  method: "password",
  user: { username: "glowpod:admin" },
  hub: { role: "admin" },
};

const settings: HubSettings = {
  serverURL: "https://hub.example.com",
  redirectURL: "https://hub.example.com/auth/oidc/callback",
  fixed: { oidc: false, ldap: true },
  auth: {
    oidc: {
      enabled: true,
      name: "Okta",
      issuer: "https://example.okta.com",
      clientId: "glowpod",
      clientSecretSet: true,
      scopes: ["openid", "email"],
      usernameClaim: "email",
      groupsClaim: "groups",
    },
    ldap: {
      enabled: false,
      url: "",
      bindDN: "",
      bindPasswordSet: false,
      userBase: "",
      userFilter: "(uid={username})",
      usernameAttribute: "uid",
      groupBase: "",
      groupFilter: "(member={dn})",
      groupAttribute: "cn",
      startTLS: false,
      ca: "",
    },
  },
};

interface Sent {
  method: string;
  path: string;
  body: unknown;
}

function setup(session: Session) {
  const sent: Sent[] = [];
  renderApp(
    "/hub/settings",
    (url, init) => {
      const method = init?.method ?? "GET";
      if (method !== "GET") {
        sent.push({
          method,
          path: url.pathname,
          body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
        });
        if (url.pathname.endsWith("/check"))
          return Response.json({ ok: true, message: "Reached it." });
        return new Response(null, { status: 204 });
      }
      switch (url.pathname) {
        case "/api/v1/session":
          return Response.json(session);
        case "/api/v1/clusters":
          return Response.json({ clusters: [] });
        case "/api/v1/hub/settings":
          return Response.json(settings);
        case "/api/v1/hub/users":
          return Response.json({ users: [{ username: "admin", role: "admin", created: "" }] });
        default:
          return undefined;
      }
    },
    { home: true },
  );
  return sent;
}

describe("Hub settings", () => {
  it("keeps secrets out, keeps typed scopes, and saves SSO", async () => {
    const user = userEvent.setup();
    const sent = setup(admin);
    const sso = await screen.findByRole("region", { name: "Single sign-on (OIDC)" });
    expect(within(sso).getByLabelText("Client secret")).toHaveValue("");
    expect(within(sso).getByText("Kept. Leave empty to keep it.")).toBeInTheDocument();

    const scopes = within(sso).getByLabelText("Scopes");
    await user.clear(scopes);
    await user.type(scopes, "openid, email, groups");
    expect(scopes).toHaveValue("openid, email, groups");

    await user.click(within(sso).getByRole("button", { name: "Check" }));
    expect(await within(sso).findByText("Reached it.")).toBeInTheDocument();
    await user.click(within(sso).getByRole("button", { name: "Save" }));
    await waitFor(() => {
      const put = sent.find((x) => x.method === "PUT" && x.path === "/api/v1/hub/settings/oidc");
      const body = put?.body as { scopes?: string[]; clientSecret?: string } | undefined;
      expect(body?.scopes).toEqual(["openid", "email", "groups"]);
      expect(body?.clientSecret).toBe("");
    });
    expect(
      await within(sso).findByText("Saved. The sign-in page shows it now."),
    ).toBeInTheDocument();
  });

  it("shows LDAP set with Helm values as read-only", async () => {
    setup(admin);
    const ldap = await screen.findByRole("region", { name: "LDAP / Active Directory" });
    expect(within(ldap).getByText(/set with Helm values/)).toBeInTheDocument();
    expect(within(ldap).queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
  });

  it("adds a Glowpod account", async () => {
    const user = userEvent.setup();
    const sent = setup(admin);
    const accounts = await screen.findByRole("region", { name: "Glowpod accounts" });
    expect(await within(accounts).findByText("admin")).toBeInTheDocument();
    expect(
      within(accounts).queryByRole("button", { name: /Delete admin/ }),
    ).not.toBeInTheDocument();
    await user.click(within(accounts).getByRole("button", { name: "Add account" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a Glowpod account" });
    await user.type(within(dialog).getByLabelText("Username"), "carol");
    await user.type(within(dialog).getByLabelText("Password"), "a long enough one");
    await user.type(within(dialog).getByLabelText("The same password again"), "a long enough one");
    await user.click(within(dialog).getByRole("button", { name: "Add account" }));
    await waitFor(() => {
      expect(sent).toContainEqual({
        method: "POST",
        path: "/api/v1/hub/users",
        body: { username: "carol", role: "user", password: "a long enough one" },
      });
    });
  });

  it("is for administrators only", async () => {
    setup({ ...admin, user: { username: "oidc:bob" }, method: "oidc", hub: { role: "user" } });
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Server" })).not.toBeInTheDocument();
    });
  });
});
