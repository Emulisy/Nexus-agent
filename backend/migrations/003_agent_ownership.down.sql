-- Remove ownership metadata without deleting the Agent records themselves.
-- The matching application version must no longer query the owner_id column.
DROP INDEX IF EXISTS idx_agents_owner;
ALTER TABLE agents DROP COLUMN IF EXISTS owner_id;
