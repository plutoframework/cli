import { Button, Input, Label, TextField } from "@heroui/react";
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
        <TextField
          key={field.name}
          name={field.name}
          type={field.type}
          isRequired
          isDisabled={pending}
          minLength={field.minLength}
          maxLength={field.maxLength}
          autoComplete={field.autoComplete}
        >
          <Label>{field.label}</Label>
          <Input />
        </TextField>
      ))}
      {!hasEmail && (
        <p className="pluto-muted">
          Paste the token from your verification or recovery message. It is sent
          only when you submit this form.
        </p>
      )}
      <Button type="submit" isDisabled={pending}>
        {pending ? "Working…" : modeLabels[mode]}
      </Button>
    </form>
  );
}
