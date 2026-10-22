import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api, ApiError, apiGet } from "./api";
import { byteScale, bytes, initials, percent, rate } from "./format";
import { applyMessage } from "./live";
import { queryKeys } from "./queryKeys";
import { formatAge } from "./time";
import type { KubeEvent } from "./types";

describe("apiGet", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("returns JSON on success", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(Response.json({ ok: true }))),
    );
    await expect(apiGet("/api/v1/session")).resolves.toEqual({ ok: true });
  });

  it.each([
    {
      name: "API error body",
      res: Response.json(
        { error: { code: "forbidden", message: "Ask an admin." } },
        { status: 403 },
      ),
      want: { status: 403, code: "forbidden", message: "Ask an admin." },
    },
    {
      name: "non-JSON error",
      res: new Response("bad gateway", { status: 502 }),
      want: { status: 502, code: "http_502", message: "Something went wrong." },
    },
  ])("throws ApiError for $name", async ({ res, want }) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(res)),
    );
    await expect(apiGet("/x")).rejects.toMatchObject(want);
  });

  it("explains when the server is down", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new TypeError("Failed to fetch"))),
    );
    const err = await apiGet("/x").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ code: "server_unreachable" });
  });

  it("builds cluster URLs with the cluster id and namespace", async () => {
    const fetchMock = vi.fn<(url: string) => Promise<Response>>(() =>
      Promise.resolve(Response.json({ events: [] })),
    );
    vi.stubGlobal("fetch", fetchMock);
    await api.events("default", "payments", 20);
    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      "/api/v1/clusters/default/events?namespace=payments&limit=20",
    );
    expect(api.streamURL("default", "")).toMatch(/^ws:\/\/.+\/api\/v1\/clusters\/default\/stream$/);
  });
});

describe("format", () => {
  it.each([
    [0.62, "62%"],
    [0.785, "79%"],
    [1, "100%"],
  ])("percent(%s) = %s", (f, want) => {
    expect(percent(f)).toBe(want);
  });

  it.each([
    [512, "512 B"],
    [1536, "1.5 KiB"],
    [64 * 1024 ** 3, "64 GiB"],
    [3.9 * 1024 ** 4, "3.9 TiB"],
  ])("bytes(%s) = %s", (n, want) => {
    expect(bytes(n)).toBe(want);
  });

  it("scales value and max together", () => {
    expect(byteScale(64 * 1024 ** 3)).toEqual({ divisor: 1024 ** 3, unit: "GiB" });
  });

  it.each([
    [120e6, "120", "MB/s"],
    [98777, "99", "kB/s"],
    [4200, "4.2", "kB/s"],
    [12, "12", "B/s"],
  ])("rate(%s) = %s %s", (n, value, unit) => {
    expect(rate(n)).toEqual({ value, unit });
  });

  it.each([
    ["kubernetes-admin", "KA"],
    ["luka", "LU"],
    ["luka.baidoshvili@gmail.com", "LB"],
    ["system:serviceaccount:glowpod:viewer", "VI"],
    ["", "?"],
  ])("initials(%s) = %s", (name, want) => {
    expect(initials(name)).toBe(want);
  });

  it.each([
    [0, "0 s"],
    [12_400, "12 s"],
    [3 * 60_000, "3 min"],
    [5 * 3_600_000, "5 h"],
    [72 * 3_600_000, "3 d"],
  ])("formatAge(%s) = %s", (ms, want) => {
    expect(formatAge(ms)).toBe(want);
  });
});

function ev(id: string, minutesAgo: number): KubeEvent {
  return {
    id,
    type: "Normal",
    reason: "Pulled",
    message: "",
    object: { kind: "Pod", name: "p" },
    count: 1,
    time: new Date(Date.now() - minutesAgo * 60_000).toISOString(),
  };
}

describe("applyMessage", () => {
  const key = queryKeys.events("default", "");

  it("adds new events newest first and replaces updated ones", () => {
    const client = new QueryClient();
    client.setQueryData(key, [ev("a", 5), ev("b", 10)]);
    applyMessage(client, "default", "", { type: "event", op: "added", event: ev("c", 1) }, vi.fn());
    applyMessage(
      client,
      "default",
      "",
      { type: "event", op: "modified", event: ev("b", 0) },
      vi.fn(),
    );
    expect(client.getQueryData<KubeEvent[]>(key)?.map((e) => e.id)).toEqual(["b", "c", "a"]);
  });

  it("leaves an unloaded event list alone", () => {
    const client = new QueryClient();
    applyMessage(client, "default", "", { type: "event", op: "added", event: ev("c", 1) }, vi.fn());
    expect(client.getQueryData(key)).toBeUndefined();
  });

  it("schedules an Overview refresh on pod changes", () => {
    const schedule = vi.fn();
    applyMessage(
      new QueryClient(),
      "default",
      "",
      {
        type: "pod",
        op: "modified",
        pod: {
          namespace: "a",
          name: "b",
          phase: "Running",
          health: "running",
          ready: "1/1",
          restarts: 0,
          createdAt: "2026-10-22T00:00:00Z",
        },
      },
      schedule,
    );
    expect(schedule).toHaveBeenCalledOnce();
  });

  it("invalidates the right query on resync", () => {
    const client = new QueryClient();
    const spy = vi.spyOn(client, "invalidateQueries");
    applyMessage(client, "default", "x", { type: "resync", resource: "events" }, vi.fn());
    applyMessage(client, "default", "x", { type: "resync", resource: "pods" }, vi.fn());
    expect(spy.mock.calls.map((c) => c[0]?.queryKey)).toEqual([
      queryKeys.events("default", "x"),
      queryKeys.overview("default", "x"),
    ]);
  });
});
