import { useActions } from "@plutoframework/frontend/react";
import type { FailureKind } from "@plutoframework/frontend/actions";
import { useId, useRef, useState } from "react";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";

type Props = {
  onSubmit?: (value: { title: string }) => Promise<void>;
  scope?: string;
  classifyError?: (error: unknown) => FailureKind;
};

export function <%.Symbol%>(props: Props) {
  return <FormContents key={props.scope ?? "catalog"} {...props} />;
}

function FormContents({ onSubmit, scope, classifyError }: Props) {
  const id = useId();
  const formRef = useRef<HTMLFormElement>(null);
  const action = useActions<{ title: string }, void>({
    scope: scope ?? "catalog",
    execute: async (value) => {
      await onSubmit?.(value);
    },
    ...(classifyError ? { classifyError } : {}),
    confirm: () => {
      formRef.current?.reset();
      setFeedback("Your action completed.");
    },
  });
  const entry = action.entries[0];
  const pending = entry?.status === "pending";
  const locked = !action.ready || pending || entry?.status === "uncertain";
  const [feedback, setFeedback] = useState("");
  return (
    <form
      ref={formRef}
      className="pluto-stack"
      aria-busy={pending}
      onSubmit={async (event) => {
        event.preventDefault();
        if (locked) {
          return;
        }
        const form = event.currentTarget;
        const value = {
          title: String(new FormData(form).get("title") ?? "").trim(),
        };
        if (!value.title) {
          setFeedback("Enter a title.");
          return;
        }
        if (!onSubmit) {
          setFeedback(
            "Connect onSubmit to your feature operation. No data has been saved.",
          );
          return;
        }
        if (!scope) {
          setFeedback("Provide an actor/session/tenant scope before connecting this form.");
          return;
        }
        setFeedback("");
        await action.run("submit", value);
      }}
    >
      <div className="pluto-field">
        <Label htmlFor={id}>Title</Label>
        <Input
          id={id}
          name="title"
          required
          maxLength={200}
          disabled={locked}
        />
      </div>
      <Button type="submit" disabled={locked}>
        {pending ? "Working…" : "Continue"}
      </Button>
      {pending && <p role="status">Working…</p>}
      {entry?.status === "uncertain" && <p role="alert">The action could not be completed with a known outcome. Refresh to verify before retrying.</p>}
      {entry?.status === "rejected" && <p role="alert">The action was rejected. Your input is preserved; correct it and retry.</p>}
      {feedback && (
        <p role="status" className="pluto-muted">
          {feedback}
        </p>
      )}
    </form>
  );
}
