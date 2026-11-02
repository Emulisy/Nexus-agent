/**
 * Fallback for unmatched authenticated routes. The return action takes the
 * user back to chat without depending on an unknown resource identifier.
 */
import { useNavigate } from 'react-router-dom';
import { EmptyState, Button } from '@/components/ui/primitives';

export function NotFoundPage() {
  const navigate = useNavigate();
  return (
    <div className="page-container">
      <EmptyState
        icon="⌀"
        title="Page not found"
        hint="This page may have been removed, or the address may be incorrect."
        action={<Button onClick={() => navigate('/chat')}>Back to chat</Button>}
      />
    </div>
  );
}
