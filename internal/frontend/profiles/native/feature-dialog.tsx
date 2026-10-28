import { useId, useRef } from "react";

export function <%.Symbol%>() {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button
        ref={trigger}
        className="pluto-button"
        onClick={() => dialog.current?.showModal()}
      >
        Open details
      </button>
      <dialog
        className="pluto-dialog"
        ref={dialog}
        aria-labelledby={id}
        onClose={() => trigger.current?.focus()}
      >
        <h2 id={id}>Consider the details</h2>
        <p>A focused place for your next decision.</p>
        <form method="dialog">
          <button className="pluto-button">Close</button>
        </form>
      </dialog>
    </>
  );
}
