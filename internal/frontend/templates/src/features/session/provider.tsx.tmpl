import { createContext, useContext } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { sessionOptions } from "./queries";
import type { Session } from "./types";

type SessionState = {
  session: Session | null;
  pending: boolean;
  error: unknown;
  refresh: () => void;
};
const SessionContext = createContext<SessionState | null>(null);
export function SessionProvider({ children }: { children: ReactNode }) {
  const query = useQuery(sessionOptions());
  return (
    <SessionContext.Provider
      value={{
        session: query.data ?? null,
        pending: query.isPending,
        error: query.error,
        refresh: () => {
          void query.refetch();
        },
      }}
    >
      {children}
    </SessionContext.Provider>
  );
}
export function useSession(): SessionState {
  const value = useContext(SessionContext);
  if (!value) throw new Error("SessionProvider is required");
  return value;
}
