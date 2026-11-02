/**
 * Controlled confirmation dialog with shared cancel/confirm actions.
 * The caller performs the mutation and closes the dialog after success. The
 * loading prop disables repeated confirmation through Button; danger changes
 * the action presentation without changing callback behavior.
 */
import type { ReactNode } from 'react';
import { Modal } from './Modal';
import { Button } from './primitives';

interface ConfirmProps {
  open: boolean;
  title: string;
  content: ReactNode;
  confirmText?: string;
  danger?: boolean;
  /** Disables the confirm button while the caller processes its mutation. */
  loading?: boolean;
  onCancel: () => void;
  /** The caller owns async work, error reporting, and when the open flag changes. */
  onConfirm: () => void;
}

export function ConfirmDialog({
  open,
  title,
  content,
  confirmText = 'Confirm',
  danger,
  loading,
  onCancel,
  onConfirm,
}: ConfirmProps) {
  return (
    <Modal
      open={open}
      title={title}
      onClose={onCancel}
      width="max-w-sm"
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant={danger ? 'danger' : 'primary'} loading={loading} onClick={onConfirm}>
            {confirmText}
          </Button>
        </>
      }
    >
      <div className="text-sm text-fog">{content}</div>
    </Modal>
  );
}
