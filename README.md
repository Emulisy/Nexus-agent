# SupaAgent

A local agent workspace: React frontend, Go net/http API and document worker, Docker dependencies, and real model APIs. All built-in interface text and prompts are English.

## Features

- Streaming chat, conversation history, reasoning, tool results, and citations.
- Host/expert agents with configurable prompts, models, tools, and knowledge bases.
- Document upload, background indexing, hybrid knowledge search, and long-term memory.
- YAML workflows with run history and human approval.
- MCP tools and A2A remote agents.
- OpenAI-compatible provider configuration, retries, and model fallback.

The repository contains no application Docker image, deployment configuration, monitoring stack, profiling server, or load-test scripts. Basic terminal logs and in-app execution history remain.

## Run locally

Requirements: Go 1.26+, Node.js 20.19+ or 22.12+, and Docker Desktop with Linux containers. The commands below work in PowerShell from the project directory. No Make installation is required.

### 1. Configure model keys

Copy `backend/.env.example` to `backend/.env` only if `.env` does not already exist. Fill in at least one provider key. Existing `.env` files and saved credentials are preserved.

```dotenv
OPENAI_API_KEY=
GEMINI_API_KEY=
DEEPSEEK_API_KEY=
```

Optional model names and base URLs are listed in `.env.example`. Use model names available to your account. The API and worker load the same environment file and encrypted provider catalog.

### 2. Start everything

On Windows, double-click **start.cmd** in the project root. Alternatively, run:

```powershell
.\start.cmd
```

The launcher starts Docker Desktop if necessary, waits for the dependency containers, builds the backend commands, applies migrations, installs frontend dependencies when needed, and starts the API, document worker, and frontend in hidden background processes.

Open **http://localhost:5173** when the launcher reports that SupaAgent is ready. The API uses port 8080. The launcher window can be closed after startup; the services remain running. On a new database, save the administrator password printed during migration before closing the window. Existing users retain their credentials except for the legacy default-password rotation.

Double-click **stop.cmd** to stop the application and its dependency containers. Data volumes and credentials are preserved. Status and optional settings are available through PowerShell:

```powershell
.\dev.ps1 status
.\dev.ps1 start -Search
.\dev.ps1 stop
.\dev.ps1 stop -KeepDependencies
```

Run stop before changing startup options. If script execution is restricted, use the CMD launchers. For custom ports:

```powershell
.\start.cmd -ApiPort 8090 -FrontendPort 5174
```

Runtime binaries, process IDs, dependency fingerprints, and logs are stored in the ignored `.local` directory. Read `.local/logs/api.error.log`, `worker.error.log`, or `frontend.error.log` if a service fails. Startup rejects occupied application ports and cleans up newly launched processes on failure. Repeated start commands reuse a running instance.

### 3. Configure knowledge search and memory

Chat uses the configured chat API directly. Document search and long-term memory also require an **embedding model**; a chat-only model does not provide embeddings.

In Models, register a provider-supported model with type Embedding. Set `llm.default_embedding` in `backend/configs/config.yaml` to its alias, and set `milvus.dim` to the vector dimension returned by that model. Restart the API and worker before uploading documents. Reranking is optional. Existing collections keep their original dimension; create a new knowledge base when changing embedding dimensions.

## Optional tools

Enable local web search with:

```powershell
# From backend
docker compose --profile search up -d --wait
```

SearXNG serves the built-in `web_search` tool on port 8081. MCP/A2A integrations are configured through Integrations. Docker code execution remains optional and disabled by default; enable `sandbox` only after supplying an image with the runtimes you need. No sandbox image build pipeline is included.

## Stop and check

Use `stop.cmd` to stop all local services. To keep Docker dependencies running, use `.\dev.ps1 stop -KeepDependencies`.

Checks:

```powershell
# From backend
go build ./...
docker compose config --quiet
# From frontend
npm run build
```

## Project structure

```text
start.cmd           One-click Windows startup
stop.cmd            Stop local services without deleting data
dev.ps1             Startup, shutdown, and status orchestration
backend/
  cmd/server/       HTTP API
  cmd/worker/       Background document indexing
  cmd/migrate/      Database migrations and initial admin
  configs/          Local application configuration
  docker-compose.yaml
  local/searxng/    Optional search service settings
  internal/         Agents, models, tools, RAG, memory, workflows, and persistence
  migrations/       Versioned database schema
frontend/           English React interface
```

Existing database records and volumes are not deleted by this cleanup. Historical unused tables can remain in an upgraded database; new databases create only the application tables. The Compose project name remains `nexus-agent` to reuse existing volumes. If a previous monitoring or Ollama container is still running, it is no longer managed by this file; remove that specific unused container separately without removing storage volumes.
