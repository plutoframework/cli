import { Modal, Button } from "@heroui/react";

export function <%.Symbol%>() {
  return (
    <Modal>
      <Button>Open details</Button>
      <Modal.Backdrop>
        <Modal.Container>
          <Modal.Dialog>
            <Modal.CloseTrigger />
            <Modal.Header>
              <Modal.Heading>Consider the details</Modal.Heading>
            </Modal.Header>
            <Modal.Body>
              <p>A focused place for your next decision.</p>
            </Modal.Body>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
