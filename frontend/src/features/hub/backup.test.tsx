import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Session } from "../../lib/types";
import * as fx from "../../test/fixtures";
import { renderApp } from "../../test/renderApp";

interface Sent {
  path: string;
  body: Record<string, string>;
}

function setup(path: string, session: Session) {
  const sent: Sent[] = [];
  renderApp(
    path,
    (url, init) => {
      const method = init?.method ?? "GET";
      if (method === "POST" && url.pathname === "/api/v1/hub/backup") {
        sent.push({
          path: url.pathname,
          body: JSON.parse(typeof init?.body === "string" ? init.body : "{}") as Record<
            string,
            string
          >,
        });
        return new Response('{"format":"glowpod-hub-backup"}', {
          headers: {
            "Content-Disposition": 'attachment; filename="glowpod-hub-backup-2026-10-23.json"',
          },
        });
      }
      if (method === "POST" && url.pathname === "/api/v1/hub/restore") {
        sent.push({
          path: url.pathname,
          body: JSON.parse(typeof init?.body === "string" ? init.body : "{}") as Record<
            string,
            string
          >,
        });
        return Response.json({ created: "2026-10-23T00:00:00Z", users: 2, clusters: 3 });
      }
      switch (url.pathname) {
        case "/api/v1/session":
          return Response.json(session);
        case "/api/v1/clusters":
          return Response.json({ clusters: [] });
        case "/api/v1/hub/users":
          return Response.json({ users: [] });
        case "/api/v1/hub/settings":
          return Response.json(settings);
        default:
          return undefined;
      }
    },
    { home: true },
  );
  return sent;
}

const settings = {
  serverURL: "https://hub.example.com",
  redirectURL: "",
  fixed: { oidc: true, ldap: true },
  auth: {
    oidc: {
      enabled: false,
      name: "",
      issuer: "",
      clientId: "",
      clientSecretSet: false,
      scopes: [],
      usernameClaim: "",
      groupsClaim: "",
    },
    ldap: {
      enabled: false,
      url: "",
      bindDN: "",
      bindPasswordSet: false,
      userBase: "",
      userFilter: "",
      usernameAttribute: "",
      groupBase: "",
      groupFilter: "",
      groupAttribute: "",
      startTLS: false,
      ca: "",
    },
  },
};

const admin: Session = {
  ...fx.session,
  mode: "in-cluster",
  method: "password",
  user: { username: "glowpod:admin" },
  hub: { role: "admin" },
};

describe("backup and restore", () => {
  it("downloads a backup with a passphrase typed twice", async () => {
    const user = userEvent.setup();
    const blobs = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:backup");
    const clicks = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);
    try {
      const sent = setup("/hub/settings", admin);
      const card = await screen.findByRole("region", { name: "Backup and restore" });
      await user.click(within(card).getByRole("button", { name: "Download backup" }));
      const dialog = await screen.findByRole("dialog", { name: "Download a backup" });
      await user.type(within(dialog).getByLabelText("Passphrase"), "a long passphrase");
      await user.type(
        within(dialog).getByLabelText("The same passphrase again"),
        "a different one!!",
      );
      await user.click(within(dialog).getByRole("button", { name: "Download backup" }));
      expect(
        await within(dialog).findByText("The two passphrases don't match."),
      ).toBeInTheDocument();
      expect(sent).toHaveLength(0);

      await user.clear(within(dialog).getByLabelText("The same passphrase again"));
      await user.type(
        within(dialog).getByLabelText("The same passphrase again"),
        "a long passphrase",
      );
      await user.click(within(dialog).getByRole("button", { name: "Download backup" }));
      expect(
        await within(dialog).findByText(/Saved glowpod-hub-backup-2026-10-23.json/),
      ).toBeInTheDocument();
      expect(sent).toEqual([
        { path: "/api/v1/hub/backup", body: { passphrase: "a long passphrase" } },
      ]);
      expect(clicks).toHaveBeenCalled();
    } finally {
      blobs.mockRestore();
      clicks.mockRestore();
    }
  });

  it("restores at first start and sends people to sign in", async () => {
    const user = userEvent.setup();
    const sent = setup("/setup", { ...admin, hub: { role: "admin", setupRequired: true } });
    await user.click(await screen.findByRole("button", { name: "Restore a backup instead" }));
    const form = await screen.findByRole("form", { name: "Restore a backup" });
    const file = new File(['{"format":"glowpod-hub-backup"}'], "backup.json", {
      type: "application/json",
    });
    await user.upload(within(form).getByLabelText("Backup file"), file);
    await user.type(within(form).getByLabelText("Passphrase"), "a long passphrase");
    expect(within(form).queryByLabelText(/to confirm/)).not.toBeInTheDocument();
    await user.click(within(form).getByRole("button", { name: "Restore" }));
    await waitFor(() => {
      expect(sent).toContainEqual({
        path: "/api/v1/hub/restore",
        body: { passphrase: "a long passphrase", backup: '{"format":"glowpod-hub-backup"}' },
      });
    });
    expect(await screen.findByText(/2 Glowpod accounts and 3 clusters/)).toBeInTheDocument();
  });
});
