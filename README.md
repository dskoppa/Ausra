# Aušra

Aušra is a small, self-hosted infrastructure automation control plane written in Go. This first release runs Ansible playbooks, Terraform, OpenTofu, and shell jobs from a mounted workspace. It includes sequential workflows, optional approval before a workflow, interval schedules, captured run logs, cancellation, and an audit trail.

## Run with Docker

1. Copy `.env.example` to `.env` and set a unique password of at least 12 characters.
2. Create a `workspace` directory and put your playbooks and infrastructure code there.
3. Start your Docker daemon (for example, `colima start` on macOS). If `docker compose version` works, run `docker compose up --build -d`. Otherwise use the plain Docker commands below.
4. Open <http://localhost:8080> and sign in.

Some Docker CLI installations do not include the Compose plugin; `docker compose` then reports an unknown command or flag. Use these equivalent commands in that case:

```sh
docker build -t ausra:local .
docker run -d --name ausra -p 127.0.0.1:8080:8080 --env-file .env -v ausra-data:/data -v "$PWD/workspace":/workspace --restart unless-stopped ausra:local
```

The image includes `ansible-playbook`, `terraform`, and `tofu`. The workspace is mounted at `/workspace`; job directories are relative to it. For example, an Ansible job can use directory `ansible` and one argument per line: `site.yml`, `-i`, `inventory.ini`. A Terraform job can use directory `terraform/prod` and argument `plan` or `apply`.

## Git repositories

Open **Repositories** in the UI to connect a public HTTPS Git URL. Leave the ref empty to follow the repository's default branch, or enter a branch, tag, or full commit ID. Aušra clones and syncs the repository, shows its resolved commit, and lets you select it as a job source. A Git-backed job's directory is relative to that repository. Each run records the exact source commit when requested and executes from a disposable checkout of that commit, so a later sync does not change a queued or approval-waiting run.

For GitHub repositories, select **Set up webhook** on the repository card. Aušra shows a callback URL and a new secret once. In GitHub, open the repository's **Settings → Webhooks → Add webhook**, enter the callback as the payload URL, choose `application/json`, paste the secret, select only **push** events, and activate the webhook. Aušra verifies GitHub's SHA-256 signature, matches the repository and configured branch or tag, records delivery IDs to reject duplicates, and syncs matching pushes in the background. Ping and unmatched events are acknowledged without a sync. A commit-pinned ref never advances from a push. Use **Rotate webhook secret** to replace a secret and update it in GitHub, or **Disable webhook** to remove it.

The default **On matching push** setting is **Sync only**. An admin can opt into a job that uses this repository or a workflow whose Git-backed jobs use this repository. After sync, Aušra creates a run only if the synced revision equals GitHub's `after` commit; an older push that has been superseded cannot queue a run for newer code. Every webhook-triggered run waits for admin approval, including jobs and workflows normally configured to run immediately. **Create test run** checks the target mapping and creates an approval-waiting run from the current synced revision; it does not simulate GitHub signature delivery. **View deliveries** shows recent event status, ref, commit, and any created run. To stop new runs, switch back to **Sync only**. Existing approval-waiting runs can still be approved or canceled.

GitHub must reach the callback over public HTTPS. The default Docker port binds to localhost, so use a public HTTPS reverse proxy forwarding `/hooks/github/` to Aušra and open the UI through that hostname when copying the URL. Do not expose the plain HTTP listener directly to the internet. Keep the `ausra-data` volume protected: webhook secrets are in mode-0600 files under `/data/webhook-secrets`, while delivery records live in `/data/state.json`. Duplicates within the latest 1,000 deliveries are ignored; a failed or interrupted sync can be retried by redelivery. The current Git connection does not support private credentials or submodule fetching. Do not put tokens in repository URLs. A repository job executes code in the current Aušra container; isolated remote runners are still a planned checkpoint. To rebuild and restart a plain-Docker deployment after source changes, run `docker build -t ausra:local .`, then stop and remove the `ausra` container and repeat the `docker run` command above. The `ausra-data` volume keeps repository metadata and run history.

The Docker port binds to localhost by default. Put an HTTPS reverse proxy in front before exposing it to other machines. The service runs as UID 10001, so mounted workspace files must be readable and writable by that user. Infrastructure tools may write cache or state files inside the workspace; use a remote state backend for shared Terraform/OpenTofu deployments. The data volume contains run output and configuration, and should be backed up.

## Run from source

Requires Go 1.22 or newer, plus the infrastructure CLIs you plan to invoke.

```sh
cd Ausra
export AUSRA_ADMIN_PASSWORD='a-long-unique-password'
export AUSRA_WORKSPACE="$PWD/workspace"
mkdir -p "$AUSRA_WORKSPACE"
go run .
```

The server listens on `127.0.0.1:8080` by default outside Docker. Set `AUSRA_ADDR` and `AUSRA_DATA_DIR` to change the listener and state directory.

## Current scope

This is an initial single-node release. Its admin account is supplied by the environment; all authenticated actions have admin privileges. State is stored in one JSON file with atomic replacement. Running jobs are marked `interrupted` after a restart. Logs are captured at command completion and capped to keep the state file bounded. Workflows run steps in order and stop on the first failure; approval applies to the entire workflow. Schedules use an interval in minutes, with the next run persisted in UTC.

Aušra does not yet provide team accounts, fine-grained permissions, a secrets vault, private Git credentials, policy checks, drift detection, distributed runners, or high availability. Those require additional design before this should manage production infrastructure for a team.

The path toward those capabilities is documented in [ROADMAP.md](ROADMAP.md).
The interface design and next UI checkpoints are documented in [DESIGN_SYSTEM.md](DESIGN_SYSTEM.md) and [CHECKPOINTS.md](CHECKPOINTS.md).
The library-first implementation choices and their current status are documented in [LIBRARIES.md](LIBRARIES.md).

## API

The browser uses the JSON API under `/api`. After signing in through `POST /api/login`, authenticated endpoints include `GET/POST /api/repositories`, `POST /api/repositories/{id}/sync`, `POST/DELETE /api/repositories/{id}/webhook`, `PUT/DELETE /api/repositories/{id}/trigger`, `POST /api/repositories/{id}/trigger/test`, `GET /api/repositories/{id}/deliveries`, `DELETE /api/repositories/{id}`, `GET/POST /api/jobs`, `POST /api/jobs/{id}/run`, `GET/POST /api/workflows`, `POST /api/workflows/{id}/run`, `GET/POST /api/schedules`, `GET /api/runs`, `POST /api/runs/{id}/approve`, `POST /api/runs/{id}/cancel`, and `GET /api/audit`. GitHub sends signed events to the public `POST /hooks/github/{id}` endpoint.
