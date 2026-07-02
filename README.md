- Sidecar container binary for environment
- Provides file system operations, some bash tools, etc
- Starts the underlying actor per session ID, suspends if needed
- Accepts tool call requests and returns tool call responses (OpenResponses format)


POST /environment/resume
{
   "name": "bash-env",
   "session_id": "123e4567-e89b-12d3-a456-426614174000",
   "env_variables": [...],
   "tools": [...]
}

POST /environment/suspend
{
   "session_id": "123e4567-e89b-12d3-a456-426614174000"
}

POST /environment
{
   "session_id": "123e4567-e89b-12d3-a456-426614174000"
   "inputs": [...]
}
