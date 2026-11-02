-- Initial relational schema. Timestamps use TIMESTAMPTZ and extensible payloads
-- use JSONB so the service can persist structured configuration and event data.
-- This migration is historical: tables retained here do not imply that every
-- corresponding feature has an active API or execution path in the application.

-- Users: authentication identities and the admin/user role used by API guards.
-- Store password hashes only; username and email are independently unique.
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      VARCHAR(64)  NOT NULL UNIQUE,
    email         VARCHAR(128) NOT NULL UNIQUE,
    password_hash VARCHAR(256) NOT NULL,
    role          VARCHAR(20)  NOT NULL DEFAULT 'user',
    avatar_url    VARCHAR(512) NOT NULL DEFAULT '',
    status        SMALLINT     NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Sessions group message history under a user-owned UUID. Agent selection is
-- optional; counters and last_msg_at support the paginated recent-session list.
CREATE TABLE sessions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       BIGINT      NOT NULL REFERENCES users(id),
    agent_id      BIGINT,
    title         VARCHAR(256) NOT NULL DEFAULT 'New chat',
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    pinned        BOOLEAN     NOT NULL DEFAULT FALSE,
    message_count INT         NOT NULL DEFAULT 0,
    token_usage   BIGINT      NOT NULL DEFAULT 0,
    last_msg_at   TIMESTAMPTZ,
    metadata      JSONB       NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_sessions_user ON sessions(user_id, last_msg_at DESC);

-- Messages retain user input and completed assistant output. Structured tool
-- calls, retrieval references, and trace metadata accompany the text when present.
-- Token and timing columns are accounting fields; their presence does not imply
-- that every provider or execution path populates every measurement.
CREATE TABLE messages (
    id                BIGSERIAL PRIMARY KEY,
    session_id        UUID        NOT NULL REFERENCES sessions(id),
    role              VARCHAR(20) NOT NULL,
    content           TEXT        NOT NULL,
    reasoning_content TEXT,
    tool_calls        JSONB,
    tool_call_id      VARCHAR(128),
    refs              JSONB,
    model_name        VARCHAR(64),
    prompt_tokens     INT         NOT NULL DEFAULT 0,
    completion_tokens INT         NOT NULL DEFAULT 0,
    first_token_ms    INT         NOT NULL DEFAULT 0,
    latency_ms        INT         NOT NULL DEFAULT 0,
    status            SMALLINT    NOT NULL DEFAULT 1,
    metadata          JSONB       NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_messages_session ON messages(session_id, created_at);

-- Model providers define OpenAI-compatible endpoints and encrypted API credentials.
-- Individual model choices live in model_configs and refer back to this record.
CREATE TABLE model_providers (
    id         BIGSERIAL PRIMARY KEY,
    code       VARCHAR(32)  NOT NULL UNIQUE,
    name       VARCHAR(64)  NOT NULL,
    base_url   VARCHAR(512) NOT NULL,
    api_key    TEXT         NOT NULL DEFAULT '',
    status     SMALLINT     NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Model configurations separate provider model identifiers from application aliases.
-- Type, capability flags, status, and priority are available to model resolution;
-- pricing fields are stored metadata, independent of runtime cost calculation.
CREATE TABLE model_configs (
    id             BIGSERIAL PRIMARY KEY,
    provider_id    BIGINT      NOT NULL REFERENCES model_providers(id),
    model_name     VARCHAR(64) NOT NULL,
    alias          VARCHAR(64) NOT NULL,
    type           VARCHAR(16) NOT NULL,
    context_window INT         NOT NULL DEFAULT 8192,
    max_output     INT         NOT NULL DEFAULT 4096,
    input_price    NUMERIC(12,6) NOT NULL DEFAULT 0,
    output_price   NUMERIC(12,6) NOT NULL DEFAULT 0,
    priority       INT         NOT NULL DEFAULT 100,
    capabilities   JSONB       NOT NULL DEFAULT '[]',
    is_default     BOOLEAN     NOT NULL DEFAULT FALSE,
    status         SMALLINT    NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_model_configs_type ON model_configs(type, priority);

-- Agents bind model settings, persona, tool declarations, memory options, and KB IDs.
-- The JSONB lists refer to configured resources without relational join tables.
-- User ownership is added separately by migration 003.
CREATE TABLE agents (
    id                BIGSERIAL PRIMARY KEY,
    name              VARCHAR(128) NOT NULL,
    description       TEXT,
    type              VARCHAR(20)  NOT NULL DEFAULT 'expert',
    system_prompt     TEXT,
    model_config_id   BIGINT      NOT NULL REFERENCES model_configs(id),
    fallback_model_config_id BIGINT REFERENCES model_configs(id),
    temperature       REAL        NOT NULL DEFAULT 0.7,
    top_p             REAL        NOT NULL DEFAULT 0.9,
    max_tokens        INT         NOT NULL DEFAULT 4096,
    max_iterations    INT         NOT NULL DEFAULT 10,
    memory_enabled    BOOLEAN     NOT NULL DEFAULT TRUE,
    memory_window     INT         NOT NULL DEFAULT 20,
    memory_long_enabled BOOLEAN   NOT NULL DEFAULT FALSE,
    kb_ids            JSONB       NOT NULL DEFAULT '[]',
    tools             JSONB       NOT NULL DEFAULT '[]',
    is_preset         BOOLEAN     NOT NULL DEFAULT FALSE,
    config            JSONB       NOT NULL DEFAULT '{}',
    status            SMALLINT    NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Add the optional session-to-agent reference after both tables exist.
ALTER TABLE sessions ADD CONSTRAINT fk_sessions_agent
    FOREIGN KEY (agent_id) REFERENCES agents(id);

-- Tool definitions supply the model-visible name, description, and JSON Schema.
-- Handler metadata selects the implementation configured by the service layer;
-- is_dangerous records a classification rather than implementing an approval gate.
CREATE TABLE tools (
    id            BIGSERIAL PRIMARY KEY,
    code          VARCHAR(64)  NOT NULL UNIQUE,
    name          VARCHAR(128) NOT NULL,
    type          VARCHAR(20)  NOT NULL,
    description   TEXT         NOT NULL,
    parameters    JSONB        NOT NULL,
    handler       VARCHAR(64),
    mcp_server_id BIGINT,
    endpoint      VARCHAR(512),
    auth_config   JSONB,
    default_config JSONB      NOT NULL DEFAULT '{}',
    timeout_ms    INT          NOT NULL DEFAULT 30000,
    is_dangerous  BOOLEAN      NOT NULL DEFAULT FALSE,
    status        SMALLINT     NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- MCP connection records retain transport settings, connection health, and cached
-- tool descriptions. Secret-bearing environment/header fields are structured JSON.
CREATE TABLE mcp_servers (
    id             BIGSERIAL PRIMARY KEY,
    name           VARCHAR(64)  NOT NULL,
    transport      VARCHAR(20)  NOT NULL,
    url            VARCHAR(512) NOT NULL DEFAULT '',
    command        VARCHAR(512) NOT NULL DEFAULT '',
    args           JSONB        NOT NULL DEFAULT '[]',
    env            JSONB        NOT NULL DEFAULT '{}',
    headers        JSONB        NOT NULL DEFAULT '{}',
    status         VARCHAR(20)  NOT NULL DEFAULT 'disconnected',
    last_health_at TIMESTAMPTZ,
    tools_cache    JSONB        NOT NULL DEFAULT '[]',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
-- MCP-backed definitions can refer to their source server once it has been created.
ALTER TABLE tools ADD CONSTRAINT fk_tools_mcp_server
    FOREIGN KEY (mcp_server_id) REFERENCES mcp_servers(id);

-- Remote A2A agents retain discovery-card data and endpoint health separately
-- from local Agent definitions. Calls use the registered remote base URL.
CREATE TABLE a2a_agents (
    id             BIGSERIAL PRIMARY KEY,
    name           VARCHAR(128) NOT NULL,
    base_url       VARCHAR(512) NOT NULL,
    card           JSONB        NOT NULL,
    status         VARCHAR(20)  NOT NULL DEFAULT 'unknown',
    last_health_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Knowledge bases are owned by users and map document collections to Milvus names.
-- Per-KB chunk settings and embedding metadata are retained with the collection.
CREATE TABLE knowledge_bases (
    id                 BIGSERIAL PRIMARY KEY,
    name               VARCHAR(128) NOT NULL,
    description        TEXT,
    user_id            BIGINT      NOT NULL REFERENCES users(id),
    embedding_config_id BIGINT     NOT NULL REFERENCES model_configs(id),
    chunk_size         INT         NOT NULL DEFAULT 512,
    chunk_overlap      INT         NOT NULL DEFAULT 64,
    parser_config      JSONB       NOT NULL DEFAULT '{}',
    doc_count          INT         NOT NULL DEFAULT 0,
    milvus_collection  VARCHAR(128) NOT NULL,
    status             VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Documents track original uploads by MinIO object key and ingestion status.
-- The worker updates parse/index progress, counts, and diagnostic errors;
-- the source bytes remain in object storage rather than this table.
CREATE TABLE documents (
    id           BIGSERIAL PRIMARY KEY,
    kb_id        BIGINT       NOT NULL REFERENCES knowledge_bases(id),
    filename     VARCHAR(256) NOT NULL,
    file_type    VARCHAR(16)  NOT NULL,
    object_key   VARCHAR(512) NOT NULL,
    file_size    BIGINT       NOT NULL DEFAULT 0,
    source_url   VARCHAR(512) NOT NULL DEFAULT '',
    status       VARCHAR(20)  NOT NULL DEFAULT 'pending',
    error        VARCHAR(512) NOT NULL DEFAULT '',
    chunk_count  INT          NOT NULL DEFAULT 0,
    token_count  INT          NOT NULL DEFAULT 0,
    enable_graph BOOLEAN      NOT NULL DEFAULT FALSE,
    metadata     JSONB        NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_documents_kb ON documents(kb_id, status);

-- Document chunks store searchable text and page metadata. Generated SQL IDs are
-- also used as vector primary keys so retrieval hits can be resolved back to text.
CREATE TABLE document_chunks (
    id          BIGSERIAL PRIMARY KEY,
    kb_id       BIGINT      NOT NULL,
    document_id BIGINT      NOT NULL REFERENCES documents(id),
    seq         INT         NOT NULL DEFAULT 0,
    content     TEXT        NOT NULL,
    token_count INT         NOT NULL DEFAULT 0,
    meta        JSONB       NOT NULL DEFAULT '{}',
    vector_id   VARCHAR(64) NOT NULL,
    status      SMALLINT    NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_chunks_doc ON document_chunks(document_id, seq);

-- Long-term memory stores user/Agent-scoped facts and retrieval metadata in SQL.
-- Embeddings live in Milvus; status supports hiding a fact without deleting it.
CREATE TABLE memories (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users(id),
    agent_id    BIGINT      REFERENCES agents(id),
    content     VARCHAR(1024) NOT NULL,
    vector_id   VARCHAR(64) NOT NULL,
    scope       VARCHAR(16) NOT NULL DEFAULT 'user',
    importance  SMALLINT    NOT NULL DEFAULT 5,
    hit_count   INT         NOT NULL DEFAULT 0,
    last_hit_at TIMESTAMPTZ,
    status      SMALLINT    NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_memories_user ON memories(user_id, agent_id, status);

-- Workflow definitions keep the editable YAML DSL and its version under a creator.
-- Definition activation and record status are separate from execution status.
CREATE TABLE workflows (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(128) NOT NULL,
    description TEXT,
    dsl        TEXT         NOT NULL,
    version    INT          NOT NULL DEFAULT 1,
    is_active  BOOLEAN      NOT NULL DEFAULT FALSE,
    creator_id BIGINT       NOT NULL REFERENCES users(id),
    status     SMALLINT     NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- One run captures the workflow input, final output or error, and execution timing.
-- Node-level snapshots are stored separately so run listings stay lightweight.
CREATE TABLE workflow_runs (
    id           BIGSERIAL PRIMARY KEY,
    workflow_id  BIGINT      NOT NULL REFERENCES workflows(id),
    trigger_type VARCHAR(20) NOT NULL DEFAULT 'manual',
    status       VARCHAR(24) NOT NULL DEFAULT 'running',
    input        JSONB       NOT NULL DEFAULT '{}',
    output       JSONB,
    error        VARCHAR(512) NOT NULL DEFAULT '',
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_workflow_runs_status ON workflow_runs(status);

-- Step snapshots associate a stable DSL node key with its input, output, and state.
-- Their run_id links them to the parent execution, including paused human steps.
CREATE TABLE workflow_step_runs (
    id          BIGSERIAL PRIMARY KEY,
    run_id      BIGINT      NOT NULL REFERENCES workflow_runs(id),
    node_key    VARCHAR(64) NOT NULL,
    node_type   VARCHAR(24) NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'running',
    input       JSONB,
    output      JSONB,
    error       VARCHAR(512) NOT NULL DEFAULT '',
    tokens      INT         NOT NULL DEFAULT 0,
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);
CREATE INDEX idx_step_runs_run ON workflow_step_runs(run_id);
