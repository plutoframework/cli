import { useState } from "react";
import { Button } from "@/shared/ui/button";
import { errorMessage } from "@/api/errors";
import { useAuthentication, useSignOut } from "../mutations";
import { useSession } from "../provider";
import { AuthForm, modeLabels } from "../forms/AuthForm";
import type { AuthMode } from "../types";

export function AuthPage() {
  const [mode, setMode] = useState<AuthMode>("signin");
  const [message, setMessage] = useState("");
  const { session, pending, error, refresh } = useSession();
  const mutation = useAuthentication();
  const signOut = useSignOut();
  return (
    <section className="pluto-section pluto-auth">
      <p className="pluto-eyebrow">WORKSPACE / ACCOUNT</p>
      <h1>{session ? "Welcome back." : modeLabels[mode]}</h1>
      <p className="pluto-muted">A secure session. A clear place to begin.</p>
      {pending ? (
        <p role="status">Checking your session…</p>
      ) : error ? (
        <div className="pluto-notice pluto-error" role="alert">
          {errorMessage(error)} <button onClick={refresh}>Try again</button>
        </div>
      ) : session ? (
        <div className="pluto-stack">
          <p className="pluto-notice">
            Signed in as <code>{session.actor_id}</code>. Your account session
            does not grant business permissions or tenant membership.
          </p>
          {signOut.error && (
            <p role="alert" className="pluto-notice pluto-error">
              {errorMessage(signOut.error)}
            </p>
          )}
          <Button
            disabled={signOut.isPending}
            onClick={() => {
              void signOut.mutateAsync().catch(() => undefined);
            }}
          >
            Sign out
          </Button>
        </div>
      ) : (
        <>
          {mutation.error && (
            <p className="pluto-notice pluto-error" role="alert">
              {errorMessage(mutation.error)}
            </p>
          )}
          {message && (
            <p className="pluto-notice" role="status">
              {message}
            </p>
          )}
          <AuthForm
            key={mode}
            mode={mode}
            pending={mutation.isPending}
            onSubmit={async (input) => {
              setMessage("");
              try {
                await mutation.mutateAsync({ mode, input });
                if (mode !== "signin")
                  setMessage(
                    mode === "verify"
                      ? "Confirmation complete. You can now sign in."
                      : mode === "reset"
                        ? "Password reset complete. You can now sign in."
                        : "If this request is eligible, a message will be sent with the next steps.",
                  );
              } catch {
                /* Render the mutation's sanitized error above. */
              }
            }}
          />
          <nav className="pluto-auth-nav" aria-label="Account actions">
            {(Object.keys(modeLabels) as AuthMode[])
              .filter((value) => value !== mode)
              .map((value) => (
                <button
                  key={value}
                  disabled={mutation.isPending}
                  onClick={() => {
                    setMode(value);
                    setMessage("");
                    mutation.reset();
                  }}
                >
                  {modeLabels[value]}
                </button>
              ))}
          </nav>
        </>
      )}
    </section>
  );
}
