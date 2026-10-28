import { useState } from "react";

export function <%.Symbol%>({ onAction }: { onAction?: () => Promise<void> }) {
  const [pending, setPending] = useState(false);
  const [feedback, setFeedback] = useState("");
  const activate = async () => {
    if (pending) return;
    if (!onAction) {
      setFeedback("Connect onAction to your feature operation.");
      return;
    }
    setPending(true);
    try {
      await onAction();
      setFeedback("Your action completed.");
    } catch {
      setFeedback("The action could not be completed.");
    } finally {
      setPending(false);
    }
  };
  return (
    <div className="pluto-stack">
      <button
        className="pluto-button"
        disabled={pending}
        onClick={() => {
          void activate();
        }}
      >
        {pending ? "Working…" : "Continue"}
      </button>
      {feedback && (
        <p role="status" className="pluto-muted">
          {feedback}
        </p>
      )}
    </div>
  );
}
