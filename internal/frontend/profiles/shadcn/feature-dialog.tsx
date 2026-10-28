import { Button } from "@/shared/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/shared/ui/dialog";

export function <%.Symbol%>() {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button>Open details</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Consider the details</DialogTitle>
          <DialogDescription>
            A focused place for your next decision.
          </DialogDescription>
        </DialogHeader>
      </DialogContent>
    </Dialog>
  );
}
