# Agent Substrate Environment

A lightweight **environment service** for [Agent Substrate](https://github.com/agent-substrate/substrate). It exposes a small API that lets an agent runtime
run tools — file operations and shell commands — inside session-tenant sandboxed actors.

Each session maps to a sandboxed **actor** in Agent Substrate.
This service manages that actor's lifecycle (create → resume → suspend) and
translates incoming tool calls into operations executed inside the actor. It returns
tool call responses.

---

## Overview

```mermaid
flowchart LR
    runtime["Agent Runtime"]
    env["Agent Substrate Environment"]
    substrate["Agent Substrate Control API"]
    actor["Actor (sandbox)"]

    runtime -->|resume, suspend, execute| env
    env -->|actor lifecycle operations| substrate
    env -->|tool call operations| actor
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

  # Namespace used to create/resume actors
  namespace: "default"

# Predefined environments mapping client-facing names to Agent Substrate templates.
environments:
  - name: "bash-env"
    template: "bash-env-template"
    tools:
      - "bash"
      - "read_file"
      - "write_file"
      - "list_dir"
```

| Field           | Default            | Description                                             |
| --------------- | ------------------ | ------------------------------------------------------- |
| `listen`        | `:8080`            | Bind address. A bare port (e.g. `8080`) is auto-prefixed with `:`. |
| `ate.ateapi`    | `ateapi.ate-system.svc.cluster.local:443` | Agent Substrate gRPC address (create/resume/suspend actors). |
| `ate.namespace` | `default`          | Actor template namespace.                               |
| `environments`  | `bash-env` -> `bash-env-template` | List of predefined client-facing environment to Agent Substrate template mappings. |

---

## API

All endpoints accept and return JSON.

### `POST /environment/resume`

Create (if needed) and resume the actor for a session.

```json
{
  "name": "bash-env",
  "session_id": "123e4567-e89b-12d3-a456-426614174000"
}
```

- `name` — actor template name (**required**)
- `session_id` — unique session identifier (**required**)

**Response:** `{ "status": "ok" }`


### `POST /environment/suspend`

Suspend the actor and evict the session.

```json
{ "session_id": "123e4567-e89b-12d3-a456-426614174000" }
```

**Response:** `{ "status": "ok" }`


### `POST /environment/{env_name}`

Execute one or more tool calls in the session's actor. The session must have been resumed first. Only tools configured/enabled for the `{env_name}` can be executed.

```json
{
  "session_id": "123e4567-e89b-12d3-a456-426614174000",
  "env_variables": [
    { "name": "MY_SECRET", "value": "c3ebfdfdk12345..." }
  ],
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

- `session_id` — unique session identifier (**required**)
- `env_variables` — env vars merged into command executions for this call
- `inputs` — list of tool calls to execute (**required**)

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

---

## Supported tools

Each tool call is translated into a shell command executed inside the actor. Arguments are passed via environment variables where possible to avoid shell injection.

| Tool          | Arguments                    | Behavior                                            |
| ------------- | ---------------------------- | --------------------------------------------------- |
| `bash`        | `command` (or `code`/`cmd`)  | Runs the command with `sh -c`.                      |
| `read_file`   | `path`                       | `cat` the file.                                     |
| `write_file`  | `path`, `content`            | Creates parent dirs and writes the content.         |
| `list_dir`    | `path` (defaults to `.`)     | `ls -la` the directory.                             |

---

## Example: end-to-end with curl

```bash
# 1. Resume the session
curl -sX POST localhost:8080/environment/resume \
  -H 'Content-Type: application/json' \
  -d '{"name":"bash-env","session_id":"123e4567-e89b-12d3-a456-426614174000"}'

# 2. Run a tool call with env vars
curl -sX POST localhost:8080/environment/bash-env \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"123e4567-e89b-12d3-a456-426614174000","env_variables":[{"name":"MY_SECRET","value":"c3ebfdfdk12345..."}],"inputs":[{"call_id":"c1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"uname -a\"}"}}]}'

# 3. Suspend when done
curl -sX POST localhost:8080/environment/suspend \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"123e4567-e89b-12d3-a456-426614174000"}'
```

---

## Notes & limitations

- The service is completely **stateless** — restarting the service is safe and does not lose any session state (actors persist in Agent Substrate).
- A non-zero exit code from a command is surfaced as an error in the tool response `content`.

## License

Apache 2.0 — see the license headers in source files.
