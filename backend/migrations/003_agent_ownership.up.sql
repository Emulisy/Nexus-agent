-- Add nullable ownership so existing Agent rows can be migrated before assignment.
-- User deletion cascades to owned Agents; later service queries filter by owner_id.
ALTER TABLE agents
    ADD COLUMN owner_id BIGINT REFERENCES users(id) ON DELETE CASCADE;

-- Backfill unowned records to the earliest administrator when one exists.
-- If no administrator exists at migration time, the existing NULL values remain.
UPDATE agents
SET owner_id = (SELECT id FROM users WHERE role = 'admin' ORDER BY id ASC LIMIT 1)
WHERE owner_id IS NULL
  AND EXISTS (SELECT 1 FROM users WHERE role = 'admin');

-- Match the owner-filtered Agent list and lookup paths.
CREATE INDEX idx_agents_owner ON agents(owner_id);
