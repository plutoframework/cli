import { useId, useState } from "react";

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
      <label className="pluto-field" htmlFor={id}>
        Title
        <input
          className="pluto-input"
          id={id}
          name="title"
          required
          maxLength={200}
          disabled={pending}
        />
      </label>
      <button className="pluto-button" type="submit" disabled={pending}>
        {pending ? "Working…" : "Continue"}
      </button>
      {feedback && (
        <p role="status" className="pluto-muted">
          {feedback}
        </p>
      )}
    </form>
  );
}
