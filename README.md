# Agent Substrate Environment

A lightweight **agentic environment service** for [Agent Substrate](https://github.com/agent-substrate/substrate). It exposes an API that lets an agent runtime
run tools — file operations and shell commands — inside session-tenant sandboxed actors.

Each session maps to a sandboxed actor in Agent Substrate.
The service manages the actor's lifecycle (create → resume → suspend)
via the Agent Substrate control API, and executes incoming tool calls in-process
against the local environment. It returns tool call responses.

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

## Configuration

Configuration is loaded from `config.yaml` in the working directory. If the file is missing, built-in defaults are used. Any field left empty falls back to its default.

```yaml
# Address/port for this HTTP service to listen on
listen: ":7777"

# Directory holding agentic skills, exposed via list_skills/activate_skill.
skills_dir: "skills"

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
      - "list_skills"
      - "activate_skill"
```

| Field           | Default            | Description                                             |
| --------------- | ------------------ | ------------------------------------------------------- |
| `listen`        | `:7777`            | Bind address.|
| `skills_dir`    | `skills`           | Directory holding agentic skills (see below).           |
| `environments`  | `bash-env` -> `bash-env-template` | List of predefined client-facing environment to Agent Substrate template mappings. |
| `ate.ateapi`    | `ateapi.ate-system.svc.cluster.local:443` | Agent Substrate Control API endpoint.|
| `ate.atespace`  | `default`          | Actor template atespace.                                |


## Usage

```bash
# TODO: Update with Substrate deployment instructions.
ate-env serve --config config.yaml
```

## API

### `POST /v1/environments/{env}/sessions/{session_id}/resume`

Create (if needed) and resume the actor for the session in `{env}`.

**Response:** `{ "status": "ok" }`

### `POST /v1/environments/{env}/sessions/{session_id}/suspend`

Suspend the session's actor.

**Response:** `{ "status": "ok" }`


### `POST /v1/environments/{env}/sessions/{session_id}`

Execute a tool call in the session's actor. The session must have been resumed first. Only tools configured/enabled for `{env}` can be executed. The tool call is inlined into the request body alongside `env_variables`.

```json
{
  "env_variables": [
    { "name": "MY_SECRET", "value": "c3ebfdfdk12345..." }
  ],
  "call_id": "call_1",
  "type": "function_call",
  "function": {
    "name": "bash",
    "arguments": "{\"command\": \"echo hi && ls\"}"
  }
}
```

**Response:** the tool output:

```json
{
  "type": "function_call_output",
  "name": "bash",
  "call_id": "call_1",
  "output": "hi\n..."
}
```

## Supported tools

All tool calls run in-process in this binary. The `bash` tool executes the command locally with `sh -c` via `os/exec`. File operation tools (`read_file`, `write_file`, `list_dir`) use the Go standard library directly — they never shell out.

| Tool          | Arguments                    | Behavior                                            |
| ------------- | ---------------------------- | --------------------------------------------------- |
| `bash`        | `command` | Runs the command locally with `sh -c` (`os/exec`); per-call env vars are merged in. |
| `read_file`   | `path` | Reads and returns the file contents (`os.ReadFile`). |
| `write_file`  | `path`, `content` | Creates parent dirs (`os.MkdirAll`) and writes the content (`os.WriteFile`). |
| `list_dir`    | `path` | Lists the directory (`os.ReadDir`), `ls -la` style. |
| `list_skills` | — | Lists the available agentic skills with their descriptions. |
| `activate_skill` | `name` | Returns the skill's full SKILL.md instructions and its bundled files. |

## Agentic skills

The service supports [Agent Skills](https://agentskills.io) out of the box. A skill is a subdirectory of `skills_dir` containing a `SKILL.md` file — YAML frontmatter (`name`, `description`) followed by markdown instructions — plus any bundled files the instructions reference:

```
skills/
└── pdf-processing/
    ├── SKILL.md
    └── extract.sh
```

Skills follow progressive disclosure: `list_skills` returns only each skill's name and description so the agent can pick one cheaply, and `activate_skill` loads the chosen skill's full instructions along with the paths of its bundled files, which the agent can then `read_file` or execute with `bash`. Both tools must be listed in an environment's `allowed_tools` to be callable.

## Example: end-to-end with curl

```bash
export SESSION_ID=123e4567-e89b-12d3-a456-426614174000

# 1. Resume the session
curl -sX POST localhost:7777/v1/environments/bash-env/sessions/$SESSION_ID/resume

# 2. Run a tool call with env vars
curl -sX POST localhost:7777/v1/environments/bash-env/sessions/$SESSION_ID \
  -H 'Content-Type: application/json' \
  -d '{"env_variables":[{"name":"MY_SECRET","value":"c3ebfdfdk12345..."}],"call_id":"c1","type":"function_call","function":{"name":"bash","arguments":"{\"command\":\"uname -a\"}"}}'

# 3. Suspend when done
curl -sX POST localhost:7777/v1/environments/bash-env/sessions/$SESSION_ID/suspend
```

## License

Apache 2.0.
