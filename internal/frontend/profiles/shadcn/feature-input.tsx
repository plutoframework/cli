import { useId, useState } from "react";
import { Input } from "@/shared/ui/input";

export function <%.Symbol%>() {
  const id = useId();
  const [value, setValue] = useState("");
  return (
    <div className="pluto-stack">
      <label htmlFor={id} className="pluto-field">
        Title
        <Input
          id={id}
          value={value}
          onChange={(event) => setValue(event.target.value)}
          maxLength={200}
        />
      </label>
      <span className="pluto-muted">{value.length} / 200 characters</span>
    </div>
  );
}
