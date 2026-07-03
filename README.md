# Agent Substrate Environment

A lightweight **agentic environment service** for [Agent Substrate](https://github.com/agent-substrate/substrate). It exposes an API that lets an agent runtime
run tools — file operations and shell commands — inside session-tenant sandboxed actors.

Each session maps to a sandboxed actor in Agent Substrate.
The service manages the actor's lifecycle (create → resume → suspend)
via the Agent Substrate control API, and executes incoming tool calls in-process
against the local environment. It returns tool call responses.

---

## Overview

```mermaid
%%{init: {"flowchart": {"diagramPadding": 80}}}%%
flowchart LR
    runtime["Agent Runtime"]
    env["Agent Substrate Environment"]
    substrate["Agent Substrate Control API"]
    actor["Actor (sandbox)"]

    runtime -->|resume, suspend, execute| env
    env -->|actor lifecycle operations| substrate
    env -->|tool call operations| actor
    actor --> filesystem
    actor --> bash
    actor --> web_fetcher
```

---

## Configuration

Configuration is loaded from `config.yaml` in the working directory. If the file is missing, built-in defaults are used. Any field left empty falls back to its default.

```yaml
# Address/port for this HTTP service to listen on
listen: ":7777"

# Agent Substrate configuration.
ate:
  ateapi: "ateapi.ate-system.svc.cluster.local:443"
  atespace: "default"

# Predefined environments mapping client-facing names to Agent Substrate templates.
environments:
  - name: "bash-env"
    template: "bash-env-template"
    allowed_tools:
      - "bash"
      - "read_file"
      - "write_file"
      - "list_dir"
```

| Field           | Default            | Description                                             |
| --------------- | ------------------ | ------------------------------------------------------- |
| `listen`        | `:7777`            | Bind address.|
| `ate.ateapi`    | `ateapi.ate-system.svc.cluster.local:443` | Agent Substrate Control API endpoint.|
| `ate.atespace`  | `default`          | Actor template atespace.                                |
| `environments`  | `bash-env` -> `bash-env-template` | List of predefined client-facing environment to Agent Substrate template mappings. |

---

## Usage

```bash
# TODO: Update with Substrate deployment instructions.
ate-env serve --config config.yaml
```
---

## API

All endpoints accept and return JSON. The environment (`{env}`) and session (`{session_id}`) are always part of the URL path. `GET /healthz` is served by both subcommands.

### `POST /v1/environments/{env}/sessions/{session_id}/resume`

Create (if needed) and resume the actor for the session in `{env}`.

- `{env}` — client-facing environment name, e.g. `bash-env` (**required**)
- `{session_id}` — unique session identifier (**required**)

**Response:** `{ "status": "ok" }`


### `POST /v1/environments/{env}/sessions/{session_id}/suspend`

Suspend the session's actor.

**Response:** `{ "status": "ok" }`


### `POST /v1/environments/{env}/sessions/{session_id}`

Execute one or more tool calls in the session's actor. The session must have been resumed first. Only tools configured/enabled for `{env}` can be executed.

```json
{
  "env_variables": [
    { "name": "MY_SECRET", "value": "c3ebfdfdk12345..." }
  ],
  "inputs": [
    {
      "call_id": "call_1",
      "type": "function_call",
      "function": {
        "name": "bash",
        "arguments": "{\"command\": \"echo hi && ls\"}"
      }
    }
  ]
}
```

- `env_variables` — env vars merged into command executions for this call
- `inputs` — list of tool calls to execute (**required**)

**Response:**

```json
{
  "outputs": [
    {
      "name": "bash",
      "call_id": "call_1",
      "output": "hi\n..."
    }
  ]
}
```

---

## Supported tools

All tool calls run in-process in this binary. The `bash` tool executes the command locally with `sh -c` via `os/exec`. File operation tools (`read_file`, `write_file`, `list_dir`) use the Go standard library directly — they never shell out.

| Tool          | Arguments                    | Behavior                                            |
| ------------- | ---------------------------- | --------------------------------------------------- |
| `bash`        | `command` | Runs the command locally with `sh -c` (`os/exec`); per-call env vars are merged in. |
| `read_file`   | `path` | Reads and returns the file contents (`os.ReadFile`). |
| `write_file`  | `path`, `content` | Creates parent dirs (`os.MkdirAll`) and writes the content (`os.WriteFile`). |
| `list_dir`    | `path` | Lists the directory (`os.ReadDir`), `ls -la` style. |

---

## Example: end-to-end with curl

```bash
export SESSION_ID=123e4567-e89b-12d3-a456-426614174000

# 1. Resume the session
curl -sX POST localhost:7777/v1/environments/bash-env/sessions/$SESSION_ID/resume

# 2. Run a tool call with env vars
curl -sX POST localhost:7777/v1/environments/bash-env/sessions/$SESSION_ID \
  -H 'Content-Type: application/json' \
  -d '{"env_variables":[{"name":"MY_SECRET","value":"c3ebfdfdk12345..."}],"inputs":[{"call_id":"c1","type":"function_call","function":{"name":"bash","arguments":"{\"command\":\"uname -a\"}"}}]}'

# 3. Suspend when done
curl -sX POST localhost:7777/v1/environments/bash-env/sessions/$SESSION_ID/suspend
```

---

## License

Apache 2.0 — see the license headers in source files.
