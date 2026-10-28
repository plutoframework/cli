import { renderHook, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { expect, test, vi, afterEach } from "vitest";
import { useAuthentication, useSignOut } from "./mutations";
afterEach(() => vi.unstubAllGlobals());

test("successful sign-in refreshes an otherwise fresh anonymous session", async () => {
  const client = new QueryClient();
  client.setQueryData(["session"], null);
  client.setQueryData(["scope", "old", "northstar"], ["private"]);
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (request: Request) => {
      calls.push(new URL(request.url).pathname);
      return Response.json(
        request.method === "POST"
          ? { code: "ok" }
          : {
              data: {
                id: "new-session",
                actor_id: "new-account",
                expires_at: "2030-01-01T00:00:00Z",
              },
            },
      );
    }),
  );
  const { result } = renderHook(() => useAuthentication(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
  await act(async () => {
    await result.current.mutateAsync({
      mode: "signin",
      input: {
        email: "synthetic@example.test",
        password: "synthetic-only",
        token: "",
      },
    });
  });
  expect(calls).toEqual(["/auth/signin", "/auth/session"]);
  expect(client.getQueryData(["session"])).toMatchObject({
    actor_id: "new-account",
  });
  expect(client.getQueriesData({ queryKey: ["scope"] })).toEqual([]);
  client.clear();
});

test("logout clears scoped data after the server revokes the cookie", async () => {
  const client = new QueryClient();
  client.setQueryData(["session"], { id: "session", actor_id: "actor" });
  client.setQueryData(["scope", "actor", "northstar"], ["private"]);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ code: "ok" })),
  );
  const { result } = renderHook(() => useSignOut(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
  await act(async () => {
    await result.current.mutateAsync();
  });
  expect(client.getQueryData(["session"])).toBeNull();
  expect(client.getQueriesData({ queryKey: ["scope"] })).toEqual([]);
  client.clear();
});
