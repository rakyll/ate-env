# agent-substrate-env

A lightweight **environment service** for [Agent Substrate](https://github.com/agent-substrate/substrate). It exposes a small API that lets an agent runtime
run tools — file operations and shell commands — inside session-tenant sandboxed actors.

Each session maps to a sandboxed **actor** in Agent Substrate.
This service manages that actor's lifecycle (create → resume → suspend) and
translates incoming tool calls into operations executed inside the actor. It returns
tool call responses.

---

## Overview

```
                 ┌──────────────────────────────┐
 Agent runtime   │      agent-substrate-env     │
 ─────────────►  │                              │
                 │  Session operations          │                  ┌───────────┐
   /resume  ───► │   ├─ Resume               ───┼────────────────► │   Agent   │
   /suspend ───► │   ├─ Suspend              ───┼────────────────► │ Substrate │
   /environment► │   └─ Execute tool calls   ───┼───────┐          └───────────┘
                 └──────────────────────────────┘       │
                                                        ▼
                                                   ┌───────────┐
                                                   │   Actor   │
                                                   │ (sandbox) │  → runs operations
                                                   └───────────┘
```

1. **`/environment/resume`** — creates an actor (idempotent) via Agent Substrate, resumes it, and caches the session's env vars + enabled tools in memory.
1. **`/environment/suspend`** — suspends the actor and drops the session from the in-memory cache.
1. **`/environment`** — sends tool calls to the actor and executes them to return tool responses.

---

## Configuration

Configuration is loaded from `config.yaml` in the working directory. If the file is missing, built-in defaults are used. Any field left empty falls back to its default.

```yaml
# Address/port for this HTTP service to listen on
listen: ":8080"

ate:
  # Agent Substrate ateapi gRPC server
  ateapi: "ateapi.ate-system.svc.cluster.local:443"

  # Agent Substrate atenet HTTP router
  atenet: "localhost:8000"

  # Namespace used to create/resume actors
  namespace: "default"
```

| Field           | Default            | Description                                             |
| --------------- | ------------------ | ------------------------------------------------------- |
| `listen`        | `:8080`            | Bind address. A bare port (e.g. `8080`) is auto-prefixed with `:`. |
| `ate.ateapi`    | `ateapi.ate-system.svc.cluster.local:443` | Agent Substrate gRPC address (create/resume/suspend actors). |
| `ate.atenet`    | `localhost:8000`   | HTTP router address that fronts the actors.             |
| `ate.namespace` | `default`          | Actor template namespace.                               |

> The gRPC connection to `ateapi` uses TLS with `InsecureSkipVerify` — intended for local/dev use.

---

## API

All endpoints accept and return JSON.

### `POST /environment/resume`

Create (if needed) and resume the actor for a session.

```json
{
  "name": "bash-env",
  "session_id": "123e4567-e89b-12d3-a456-426614174000",
  "env_variables": [
    { "name": "MY_SECRET", "value": "c3ebfdfdk12345..." }
  ],
  "tools": ["bash", "read_file", "write_file", "list_dir"]
}
```

- `name` — actor template name (**required**)
- `session_id` — unique session identifier (**required**)
- `env_variables` — env vars merged into every command run in this session
- `tools` — enabled tools (cached with the session)

**Response:** `{ "status": "ok" }`

### `POST /environment`

Execute one or more tool calls in the session's actor. The session must have been resumed first.

```json
{
  "session_id": "123e4567-e89b-12d3-a456-426614174000",
  "inputs": [
    {
      "call_id": "call_1",
      "type": "function",
      "function": {
        "name": "bash",
        "arguments": "{\"command\": \"echo hi && ls\"}"
      }
    }
  ]
}
```

**Response:**

```json
{
  "outputs": [
    {
      "name": "bash",
      "call_id": "call_1",
      "content": "hi\n..."
    }
  ]
}
```

### `POST /environment/suspend`

Suspend the actor and evict the session.

```json
{ "session_id": "123e4567-e89b-12d3-a456-426614174000" }
```

**Response:** `{ "status": "ok" }`

### `GET /healthz`

Liveness check → `{ "status": "healthy" }`

---

## Supported tools

Each tool call is translated into a shell command executed inside the actor. Arguments are passed via environment variables where possible to avoid shell injection.

| Tool          | Arguments                    | Behavior                                            |
| ------------- | ---------------------------- | --------------------------------------------------- |
| `bash`        | `command` (or `code`/`cmd`)  | Runs the command with `sh -c`.                      |
| `read_file`   | `path`                       | `cat` the file.                                     |
| `write_file`  | `path`, `content`            | Creates parent dirs and writes the content.         |
| `list_dir`    | `path` (defaults to `.`)     | `ls -la` the directory.                             |

Unknown tools return `Error: unsupported tool '<name>'`.

---

## Example: end-to-end with curl

```bash
SID="123e4567-e89b-12d3-a456-426614174000"

# 1. Resume the session
curl -sX POST localhost:8080/environment/resume \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"bash-env\",\"session_id\":\"$SID\",\"tools\":[\"bash\"]}"

# 2. Run a tool call
curl -sX POST localhost:8080/environment \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\":\"$SID\",\"inputs\":[{\"call_id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"uname -a\\\"}\"}}]}"

# 3. Suspend when done
curl -sX POST localhost:8080/environment/suspend \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\":\"$SID\"}"
```

---

## Notes & limitations

- Sessions are held **in memory** — restarting the service loses the cache (actors themselves persist in Agent Substrate).
- Actor calls to `/process` use a **30s** timeout.
- A non-zero exit code from a command is surfaced as an error in the tool response `content`.

## License

Apache 2.0 — see the license headers in source files.
