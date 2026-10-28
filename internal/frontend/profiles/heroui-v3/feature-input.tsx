import { useState } from "react";
import { Input, Label, TextField } from "@heroui/react";

export function <%.Symbol%>() {
  const [value, setValue] = useState("");
  return (
    <div className="pluto-stack">
      <TextField value={value} onChange={setValue} maxLength={200}>
        <Label>Title</Label>
        <Input />
      </TextField>
      <span className="pluto-muted">{value.length} / 200 characters</span>
    </div>
  );
}
