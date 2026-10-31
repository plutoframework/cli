<%if .OIDC%>import { useProviderSignIn } from "../mutations";<%end%>
<%if .OIDC%>import { useProviders } from "../queries";<%end%>
import type { ReactNode } from "react";
import { useState } from "react";
import { Button } from "@heroui/react";
import { errorMessage } from "@/api/errors";
import { useAuthentication, useSignOut } from "../mutations";
import { useSession } from "../provider";
import { AuthForm, modeLabels } from "../forms/AuthForm";
import type { AuthMode } from "../types";

<%if .OIDC%>import { ProviderSignIn } from "../components/ProviderSignIn";<%end%>

export function AuthPage() {
<%if .OIDC%>const providerSignIn=useProviderSignIn();<%end%>
<%if .OIDC%>const providers=useProviders();<%end%>
  const [mode, setMode] = useState<AuthMode>("signin");
  const [message, setMessage] = useState("");
  const { session, pending, error, refresh } = useSession();
  const mutation = useAuthentication();
  const signOut = useSignOut();
  let content: ReactNode;

  if (pending) {
    content = <p role="status">Checking your session…</p>;
  } else if (error) {
    content = (
      <div className="pluto-notice pluto-error" role="alert">
        {errorMessage(error)} <button onClick={refresh}>Try again</button>
      </div>
    );
  } else if (session) {
    content = (
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
          isDisabled={signOut.isPending}
          onPress={() => {
            void signOut.mutateAsync().catch(() => undefined);
          }}
        >
          Sign out
        </Button>
      </div>
    );
  } else {
    content = (
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
        <%if .OIDC%>{mode === "signin" && <ProviderSignIn redirecting={providerSignIn.isPending || providerSignIn.isSuccess} requestFailed={providerSignIn.isError} onContinue={()=>providerSignIn.mutate()}  available={providers.data?.includes("oidc") ?? false} pending={providers.isPending} failed={providers.isError} />}<%end%>
        <AuthForm
          key={mode}
          mode={mode}
          pending={mutation.isPending}
          onSubmit={async (input) => {
            setMessage("");

            try {
              await mutation.mutateAsync({
                mode,
                input,
              });

              if (mode !== "signin") {
                const messages: Partial<Record<AuthMode, string>> = {
                  verify: "Confirmation complete. You can now sign in.",
                  reset: "Password reset complete. You can now sign in.",
                };

                setMessage(
                  messages[mode] ??
                    "If this request is eligible, a message will be sent with the next steps.",
                );
              }
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
    );
  }

  return (
    <section className="pluto-section pluto-auth">
      <p className="pluto-eyebrow">WORKSPACE / ACCOUNT</p>
      <h1>{session ? "Welcome back." : modeLabels[mode]}</h1>
      <p className="pluto-muted">A secure session. A clear place to begin.</p>
      {content}
    </section>
  );
}
