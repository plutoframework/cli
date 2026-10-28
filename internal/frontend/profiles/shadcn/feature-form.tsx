import { useId, useState } from "react";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";

export function <%.Symbol%>({
  onSubmit,
}: {
  onSubmit?: (value: { title: string }) => Promise<void>;
}) {
  const id = useId();
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
      <div className="pluto-field">
        <Label htmlFor={id}>Title</Label>
        <Input
          id={id}
          name="title"
          required
          maxLength={200}
          disabled={pending}
        />
      </div>
      <Button type="submit" disabled={pending}>
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
