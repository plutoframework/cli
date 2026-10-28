import { useState } from "react";
import { Button, Input, Label, TextField } from "@heroui/react";

export function <%.Symbol%>({
  onSubmit,
}: {
  onSubmit?: (value: { title: string }) => Promise<void>;
}) {
  const [pending, setPending] = useState(false);
  const [feedback, setFeedback] = useState("");
  return (
    <form
      className="pluto-stack"
      onSubmit={async (event) => {
        event.preventDefault();
        if (pending) return;
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
        setPending(true);
        setFeedback("");
        try {
          await onSubmit(value);
          form.reset();
          setFeedback("Your action completed.");
        } catch {
          setFeedback("The action could not be completed. Please try again.");
        } finally {
          setPending(false);
        }
      }}
    >
      <TextField name="title" isRequired isDisabled={pending} maxLength={200}>
        <Label>Title</Label>
        <Input />
      </TextField>
      <Button type="submit" isDisabled={pending}>
        {pending ? "Working…" : "Continue"}
      </Button>
      {feedback && (
        <p role="status" className="pluto-muted">
          {feedback}
        </p>
      )}
    </form>
  );
}
