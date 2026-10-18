# Aušra roadmap

Aušra aims to combine infrastructure automation, operational runbooks, and infrastructure governance in one self-hosted product. The first release provides a usable execution core; the features below are planned, not yet implemented.

| Area | Target capability |
| --- | --- |
| Access | Team accounts, project roles, SSO/OIDC, service tokens, approval roles |
| Source | Git-connected projects, pinned revisions, inventory sources, module catalogs |
| Credentials | Encrypted secret storage, external secret providers, per-job credentials, output redaction |
| Execution | Remote runners, isolated execution environments, queues, concurrency limits, retries |
| Workflows | Branches, parallel steps, reusable subworkflows, step approvals, rollback hooks |
| Scheduling | Cron expressions, time zones, calendars, event and webhook triggers |
| Governance | Policy-as-code gates, Terraform/OpenTofu plan review, drift scans, cost estimates |
| Operations | Self-service forms, runbook templates, incident integrations, notifications |
| Reliability | SQL database, migrations, metrics, health checks, backups, high availability |

The next engineering milestone should replace the JSON state file with PostgreSQL, introduce a runner protocol, and add project-scoped identities and credentials. Those foundations are needed before multi-user approval, distributed execution, or production governance can be reliable.
