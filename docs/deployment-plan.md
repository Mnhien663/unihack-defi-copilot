# Backend Deployment Plan

## Runtime architecture

- PostgreSQL stores sessions, messages, tool calls and pending actions.
- The Go/Fiber backend calls the selected AI provider over outbound HTTPS.
- The frontend calls the backend, not the provider directly.
- No local model, Ollama installation or GPU is required.
- The manager/router integration is separate from AI provider setup.

## Environment and secrets

- Copy .env.example to .env only when .env does not already exist.
- Keep real secrets out of Git, screenshots and frontend variables.
- In hosting, inject secrets through the platform's runtime secret settings.
- Docker Compose supplies environment values when creating the container.
- Editing .env does not update an existing container automatically.
- Recreate the backend after changing provider configuration.
- DB_HOST=db inside Compose; localhost when running Go on the host.
- Use DB_SSLMODE appropriate to the deployed database.
- The local Compose database uses sslmode=disable on its Docker network.

## Provider selection

AI_PROVIDER supports mock, openai and gemini.

OpenAI requires:
- OPENAI_API_KEY
- OPENAI_MODEL

Gemini requires:
- GEMINI_API_KEY
- GEMINI_MODEL

Use only one provider for each smoke run.
Confirm that the selected model is available to the actual account
and supports the client's tool-calling request format.
Example model names in configuration are not proof of current access.

DATA_MODE=mock avoids live blockchain API calls.
DATA_MODE=data_api requires the relevant Avalanche configuration.

## Network requirements

- Backend must reach PostgreSQL.
- Live OpenAI calls require outbound HTTPS to api.openai.com.
- Live Gemini Developer API calls require outbound HTTPS to
  generativelanguage.googleapis.com.
- Live blockchain reads require outbound access to configured endpoints.
- The public deployment needs HTTPS for frontend-to-backend traffic.
- The frontend API URL must point to the deployed backend rather than
  the judge's localhost.

## Database startup

From the repository root:

```bash
docker compose up -d db
bash scripts/migrate.sh up
AI_PROVIDER=mock DATA_MODE=mock docker compose up -d --build backend
```

Run migrations explicitly before starting the application on a fresh DB.
The backend does not create schema automatically.
The migration runner tracks applied versions.
Never run rollback against the main application DB for routine testing.

## Health and chat smoke

- GET /health checks the running application and reports selected modes.
- Health success alone does not prove an AI provider request succeeds.
- POST /api/v1/chat is the injected agent endpoint.
- Required fields: session_id, wallet_address, chain_id, message, locale.
- chain_id must be 43113; locale must be vi or en.
- Use a fresh session identifier for an independent smoke test.
- Test mock mode first.
- A live-provider smoke remains a separate, potentially billable check.
- Retain HTTP status, request_id, selected provider/model and outcome.
- Do not record real keys or private user content as evidence.

## Timeouts and shutdown

- Chat middleware supplies a 30-second request context.
- Provider clients pass that context into SDK calls.
- Fiber read timeout: 10 seconds; write timeout: 35 seconds.
- HTTP shutdown timeout: 40 seconds.
- Docker backend stop grace period: 45 seconds.
- These are configured bounds, not proof every downstream operation
  handles cancellation correctly.
- Client error wrapping may prevent a provider timeout from surfacing
  as the handler's specific REQUEST_TIMEOUT response.
- SIGTERM shutdown and DB-pool closure have been verified locally.

## Quota and cost checks

Before a live run:
- Inspect the selected account/project's model access and current quota.
- Check current pricing or free-tier eligibility for that exact model.
- Review usage, billing controls and alert settings.
- Distinguish spend alerts from enforced spending limits.
- Do not assume a new API key provides a separate quota.
- Run one smoke request, inspect the result, then decide whether to retry.

Current code:
- OpenAI explicitly configures at most one SDK retry.
- Both clients request an output limit of 512 tokens per completion.
- A chat turn may make multiple completions and tool calls.
- The output limit is not a total per-chat cost cap.
- History and prompt tokens also contribute to usage.
- Gemini retry behavior is not explicitly configured in this client.
- Success logs record usage metadata and latency.
- No application-wide daily spending cap is established by this work.

## Verification status

Confirmed locally:
- Migration up/down/up on the separate test database.
- Main DB migration version 2, clean at the migration check.
- Three PostgreSQL integration tests passed.
- Test-session data persisted across PostgreSQL container restart.
- Backend health returned 200 in mock mode.
- SIGTERM produced HTTP shutdown and DB-pool closure logs.
- Mock chat smoke returned HTTP 200 with error=null.
- Request ID: bed738ed-e5c2-4197-9c90-9753641b2ca8.
- PostgreSQL contains the user and assistant messages for this request.
- The get_native_balance tool call was persisted with status succeeded.
- Blockchain output was explicitly sourced from mock_chain.

Pending:
- Live-provider smoke and account-specific quota/cost check.
- Public-host deployment, HTTPS and frontend connectivity.

## References

- https://developers.openai.com/api/reference/overview
- https://developers.openai.com/api/docs/guides/rate-limits
- https://ai.google.dev/gemini-api/docs/api-key
- https://ai.google.dev/gemini-api/docs/rate-limits
- https://ai.google.dev/gemini-api/docs/pricing