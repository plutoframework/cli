import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, authHeaders } from "@/api/client";
import { sessionOptions } from "./queries";
import type { AuthInput, AuthMode } from "./types";

export function useAuthentication() {
  const queryClient = useQueryClient();
  return useMutation({
    retry: false,
    gcTime: 0,
    mutationFn: async ({
      mode,
      input,
    }: {
      mode: AuthMode;
      input: AuthInput;
    }) => {
      const params = { header: authHeaders() };
      switch (mode) {
        case "signin":
          return api.POST("/auth/signin", {
            params,
            body: { email: input.email, password: input.password },
          });
        case "enroll":
          return api.POST("/auth/enroll", {
            params,
            body: { email: input.email, password: input.password },
          });
        case "verify":
          return api.POST("/auth/verify", {
            params,
            body: { token: input.token },
          });
        case "resend":
          return api.POST("/auth/verification", {
            params,
            body: { email: input.email },
          });
        case "recover":
          return api.POST("/auth/recovery", {
            params,
            body: { email: input.email },
          });
        case "reset":
          return api.POST("/auth/reset", {
            params,
            body: { token: input.token, password: input.password },
          });
      }
    },
    onSuccess: async (_, { mode }) => {
      if (mode === "signin" || mode === "reset") {
        await queryClient.cancelQueries();
        queryClient.removeQueries({
          predicate: (q) => q.queryKey[0] !== "session",
        });
        queryClient.setQueryData(["session"], null);
        await queryClient.fetchQuery({ ...sessionOptions(), staleTime: 0 });
      }
    },
  });
}

export function useSignOut() {
  const client = useQueryClient();
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: () =>
      api.POST("/auth/logout", { params: { header: authHeaders() } }),
    onSuccess: async () => {
      await client.cancelQueries();
      client.removeQueries({ predicate: (q) => q.queryKey[0] !== "session" });
      client.setQueryData(["session"], null);
    },
  });
}
