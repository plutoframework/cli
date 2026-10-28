import { useId } from "react";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import type { AuthInput, AuthMode } from "../types";

export const modeLabels: Record<AuthMode, string> = {
  signin: "Sign in",
  enroll: "Create an account",
  verify: "Verify your email",
  resend: "Send verification again",
  recover: "Recover access",
  reset: "Set a new password",
};
export function AuthForm({
  mode,
  pending,
  onSubmit,
}: {
  mode: AuthMode;
  pending: boolean;
  onSubmit: (input: AuthInput) => Promise<void>;
}) {
  const id = useId();
  const hasEmail = ["signin", "enroll", "resend", "recover"].includes(mode);
  const hasPassword = ["signin", "enroll", "reset"].includes(mode);
  const fields = [
    ...(hasEmail
      ? [
          {
            name: "email",
            label: "Email address",
            type: "email",
            autoComplete: "username",
            maxLength: 254,
            minLength: 1,
          },
        ]
      : []),
    ...(hasPassword
      ? [
          {
            name: "password",
            label: "Password",
            type: "password",
            autoComplete:
              mode === "signin" ? "current-password" : "new-password",
            maxLength: 1024,
            minLength: mode === "signin" ? 1 : 12,
          },
        ]
      : []),
    ...(!hasEmail
      ? [
          {
            name: "token",
            label: "Confirmation token",
            type: "password",
            autoComplete: "one-time-code",
            maxLength: 128,
            minLength: 1,
          },
        ]
      : []),
  ];
  return (
    <form
      className="pluto-stack"
      onSubmit={async (event) => {
        event.preventDefault();
        if (pending) return;
        const form = event.currentTarget;
        const data = new FormData(form);
        try {
          await onSubmit({
            email: String(data.get("email") ?? ""),
            password: String(data.get("password") ?? ""),
            token: String(data.get("token") ?? ""),
          });
        } finally {
          form.reset();
        }
      }}
    >
      {fields.map((field) => (
        <div className="pluto-field" key={field.name}>
          <Label htmlFor={`${id}-${field.name}`}>{field.label}</Label>
          <Input
            id={`${id}-${field.name}`}
            name={field.name}
            type={field.type}
            autoComplete={field.autoComplete}
            maxLength={field.maxLength}
            minLength={field.minLength}
            required
            disabled={pending}
          />
        </div>
      ))}
      {!hasEmail && (
        <p className="pluto-muted">
          Paste the token from your verification or recovery message. It is sent
          only when you submit this form.
        </p>
      )}
      <Button type="submit" disabled={pending}>
        {pending ? "Working…" : modeLabels[mode]}
      </Button>
    </form>
  );
}
