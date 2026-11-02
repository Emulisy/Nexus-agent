/**
 * Long-term memory records and explicit conversation-extraction requests.
 * Memory scope distinguishes user-wide facts from agent-specific facts.
 * Archiving changes retrieval eligibility while preserving the stored record.
 */
/**
 * Extracted long-term memory with scope, importance, access statistics, and status.
 * The optional agent ID is absent for memory that is not tied to a specific Agent.
 */
export interface Memory {
  id: number;
  user_id: number;
  agent_id?: number | null;
  content: string;
  vector_id: string;
  /** User-wide context or context restricted to an Agent. */
  scope: 'user' | 'agent';
  /** Importance score from 1 to 10. */
  importance: number;
  /** Number of recorded retrieval hits. */
  hit_count: number;
  /** Absent until the memory has been recalled. */
  last_hit_at?: string | null;
  /** 1 = active, 2 = archived. */
  status: 1 | 2;
  created_at: string;
  updated_at: string;
}

/** Identify the conversation to analyze; ownership and extraction run on the backend. */
export interface ExtractMemoryReq {
  session_id: string;
}
