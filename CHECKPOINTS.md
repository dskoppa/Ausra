# Aušra product checkpoints

This is the implementation and release checklist for a team-ready, self-hosted Aušra. Check a box only when its behavior is implemented, documented, and verified by the stated acceptance check. An unchecked box is planned work. The current single-admin prototype is described in `README.md`.

**Priority:** P0 = foundation for team use; P1 = core automation platform; P2 = advanced governance and scale. Each feature needs API, UI, permissions, audit events, documentation, and meaningful tests unless a checkpoint says otherwise.

**Implementation rule:** Prefer maintained libraries and official SDKs for common UI controls, persistence, identity, cloud, Kubernetes, secrets, and policy evaluation. See `LIBRARIES.md` for preferred choices and migration checks. Add a dependency only when implementing its feature; retain Aušra-specific workflow and visual behavior in product code.

## 0. Working baseline

- [x] Run shell, Ansible, Terraform, and OpenTofu jobs from a mounted workspace; verify each CLI starts in the Docker image.
- [x] Run sequential workflows with a whole-workflow approval gate; verify a failed step stops later steps.
- [x] Create interval schedules, capture bounded output, cancel runs, and record basic audit events.
- [ ] Replace the single admin password with named users and scoped identities before team deployment. **P0**

## UI. Immersive product experience

**Experience goal:** Aušra should feel like a live infrastructure command center. Users should see what is connected, what is running, what changed, what needs approval, and what action is safe to take next. The interface should use spatial views and restrained motion to explain relationships and state. Every visual view must have an equally usable list or detail view.

### Visual direction and design system

- [x] Define the "dawn" visual language: deep slate surfaces, warm sunrise highlights for attention, teal for healthy connections, and distinct warning/error colors. Document tokens for light and dark themes. **P0** See `DESIGN_SYSTEM.md` and `web/style.css`.
- [x] Define typography for headings, dense operational data, and monospace code/logs; set a spacing scale, component states, icon rules, and responsive breakpoints. **P0** See `DESIGN_SYSTEM.md` and `web/style.css`.
- [ ] Build reusable components for navigation, cards, tables, filters, badges, timelines, diagrams, drawers, forms, code diffs, logs, dialogs, and empty/error states. **P0**
- [ ] Migrate the prototype UI to React and TypeScript, use accessible component primitives and server-data libraries, and preserve the current screens, themes, and Docker deployment. **P0**
- [x] Make color, shape, label, and icon work together to communicate status; test that statuses remain clear without color. **P0** Run badges were checked in a grayscale Chrome rendering.
- [ ] Add meaningful transitions for opening context, moving between workflow steps, and run-state changes; respect reduced-motion preferences. **P1**
- [x] Add a persistent spacious/compact density control for the current overview and lists; verify that it survives reload and fits mobile widths. **P1**
- [ ] Apply the density control to high-volume tables when those components are built. **P1**
- [ ] Preserve user preferences for theme, density, time zone, favorite projects, and dashboard layout. **P1**

### Navigation and screen map

| Screen | Primary purpose | First useful action |
| --- | --- | --- |
| Overview | Live health, active runs, drift, approvals, and upcoming work | Open an issue or run a favorite job |
| Projects | Group automation by team and environment | Select an environment |
| Automations | Browse jobs, inventories, and workflow templates | Create or launch a job |
| Workflow studio | Build, validate, and version a workflow | Add a step and preview the path |
| Runs | Follow execution across tools and targets | Inspect a live run |
| Connections | Manage cloud, Git, SSH, and cluster access | Test a connection |
| Clusters | Explore EKS targets and workloads | Select a cluster and namespace |
| Policies | Understand gates and exceptions | Inspect a decision |
| Approvals | Review pending changes and their evidence | Approve or reject with reason |
| Audit | Trace who changed or ran what | Filter by actor, target, or run |
| Settings | SSO, teams, roles, runners, and platform health | Complete a setup task |

- [ ] Add persistent project and environment context, global search, notifications, and a command palette to the app shell. **P0**
- [ ] Keep the selected organization, project, and environment visible on every operational screen; show an unmistakable production indicator. **P0**
- [ ] Make each table searchable and filterable by status, project, environment, owner, time, and tags; preserve filters in the URL. **P0**
- [ ] Provide breadcrumbs, deep links, recent items, favorites, and back-navigation that preserves the user's place. **P1**

### Overview and live command center

- [ ] Show live run status, unhealthy connections, pending approvals, drift findings, policy failures, and next scheduled actions in one configurable overview. **P0**
  - [x] Poll active runs, pending approvals, Git source errors, and the next enabled schedule every 10 seconds.
  - [x] Save overview section visibility per browser and show refresh failures.
  - [ ] Populate drift findings, policy failures, and non-Git connection health from real checks.
- [ ] Provide a project map linking repositories, jobs, workflows, credentials, cloud accounts, and EKS clusters; selecting an item opens its detail panel. **P1**
  - [x] Link repositories, jobs, and workflows by project; open a relationship panel on selection.
  - [ ] Add managed credentials, cloud accounts, and EKS clusters when their models exist.
- [x] Make health widgets actionable: each number opens a filtered list of the exact items it counts. **P0** Overview cards open their collections; active-run and approval counts filter Runs.
- [ ] Show a recent activity timeline with actor, action, environment, outcome, and a link to the run or audit record. **P0**
  - [x] Show recent run and audit events with links to their exact records.
  - [ ] Record audit actors, environments, and outcomes as structured data instead of inferred or unavailable labels.
- [ ] Provide useful first-run and empty states with a setup path: connect source, add credentials, create job, test, then schedule. **P0**
  - [x] Guide source connection, job creation, test runs, and scheduling with relevant links and empty states.
  - [ ] Provide a managed-credential setup step when credential storage and validation exist.

### Connection setup experiences

- [ ] Build a connection catalog with AWS, Azure, GCP, Git, SSH, API, and EKS cards showing auth mode, scope, health, owner, and last validation. **P0**
- [ ] AWS wizard: choose account, credential source, target role, region, and runner; show a visual identity chain from Aušra to STS to target account. **P0**
- [ ] AWS wizard: display trust-policy and permission-policy checks separately, then run identity and least-privilege tests with clear pass/fail explanations. **P0**
- [ ] EKS wizard: choose account/role, cluster, endpoint route, and namespace; test `DescribeCluster`, API reachability, authentication, and Kubernetes RBAC as separate steps. **P0**
- [ ] EKS wizard: render the AWS role → EKS access entry → Kubernetes Role/RoleBinding path and mark the exact missing link when access fails. **P0**
- [ ] SSO wizard: enter IdP discovery URL/client details, preview redirect and group claims, test sign-in with a non-admin account, then preview role mapping before enabling. **P0**
- [ ] Keep secrets masked in forms and never reveal them in test results; provide safe copyable setup instructions for IAM and IdP administrators. **P0**
- [ ] Show connection lifecycle states (draft, validating, healthy, degraded, expired, disabled) with remediation and last successful check. **P0**

### Automation and workflow studio

- [ ] Provide a job builder with tool type, repository/revision, inventory or state backend, target environment, credentials, inputs, timeout, and runner placement. **P0**
- [ ] Show a preflight panel before launch: exact code revision, target, cloud identity, required approvals, and estimated affected resources when known. **P0**
- [ ] Build a zoomable workflow canvas with typed nodes for job, condition, parallel branch, approval, wait, notification, and rollback. **P1**
- [ ] Provide a synchronized outline beside the canvas so steps can be reordered and edited without drag-and-drop. **P1**
- [ ] Show validation directly on the graph: missing connection, unreachable branch, policy conflict, permission gap, or cycle. **P1**
- [ ] Preview possible success/failure paths and show a version diff before publishing workflow changes. **P1**
- [ ] Offer reusable templates for Terraform plan/apply, Ansible rollout, EKS deployment, incident runbook, and drift remediation. **P1**

### Live runs, plan review, and approvals

- [ ] Build a live run page with a step timeline, active-node highlight, elapsed time, target identity, artifacts, and streaming logs. **P0**
- [ ] Let users pause log following, search and filter output, jump to errors, copy a deep link, and download a redacted log. **P1**
- [ ] Show Terraform/OpenTofu plan changes grouped by create/update/delete/replace, with resource details and policy results. **P0**
- [ ] Make the approval view show the exact revision and plan hash, target account/cluster, requester, policy findings, affected resources, and expiry. **P0**
- [ ] Require a reason for rejection and for any policy override; show who approved and whether the approved artifact is still current. **P0**
- [ ] Confirm destructive actions with a clear target and impact summary; use stronger confirmation for production apply/destroy and cluster-wide actions. **P0**
- [ ] Show retry, cancel, and rollback controls only when the user's permissions and run state allow them. **P0**

### EKS operations view

- [ ] Provide cluster → namespace → workload navigation with live status, rollout history, events, and relevant run links. **P1**
- [ ] Show deployment health as a readable topology: workloads, services, ingress, dependencies, and unhealthy edges; include a list alternative. **P1**
- [ ] Compare desired and live Kubernetes objects before apply, with a clear namespace and cluster identity at the top. **P1**
- [ ] Surface permission failures, unreachable private endpoints, certificate failures, and rollout failures as distinct diagnoses with next actions. **P0**
- [ ] Link a failing workload to the workflow, Git revision, cloud role, policy decision, and last successful deployment. **P1**

### Self-service and administration

- [ ] Provide a service catalog with searchable runbooks, clear input forms, ownership, permitted environments, expected duration, and required approvals. **P1**
- [ ] Give administrators visual role and policy editors with an effective-permission preview for a selected user, project, and action. **P1**
- [ ] Give SSO, runner, credential, schedule, and retention settings dedicated setup pages with health checks and audit history. **P1**
- [ ] Add onboarding progress that can be dismissed and resumed, without blocking experienced operators. **P1**

### Responsiveness, accessibility, and UI acceptance

- [ ] Make core run inspection, approval, and incident response usable on mobile; keep complex workflow editing usable on tablet and desktop. **P1**
- [ ] Meet WCAG 2.2 AA for all core screens and test keyboard use, screen readers, contrast, focus order, zoom, and error recovery. **P0**
- [ ] Make canvas nodes, charts, and topology views keyboard navigable and provide equivalent ordered lists and tables. **P0**
- [x] Announce live run changes accessibly without flooding assistive technology; let users pause automatic updates. **P0** Polite announcements report changed status/step only, batch large updates, and the Runs view has pause/resume and manual refresh controls.
- [ ] Test light/dark themes, reduced motion, slow connections, large inventories, long logs, and small screens. **P0**
- [ ] Conduct task-based usability checks: first AWS connection, first EKS connection, SSO setup, workflow creation, plan approval, and failure diagnosis. Record where users hesitate and fix the blockers. **P0**

## 1. Product model and persistent state

- [ ] Define organizations, projects, environments, connections, credentials, inventories, jobs, workflows, runs, policies, and users with stable IDs and ownership. **P0**
- [ ] Move state from one JSON file to PostgreSQL with versioned migrations, transactions, retention, and an upgrade/rollback procedure. **P0**
- [ ] Use a PostgreSQL driver, generated typed queries, and a migration tool rather than hand-written connection, mapping, and migration machinery; verify upgrade and rollback. **P0**
- [ ] Make job submission idempotent, ensure only one worker owns a run, and recover queued/running runs after restart. **P0**
- [ ] Define API versioning, pagination, filtering, validation, error codes, and OpenAPI documentation. **P0**
- [ ] Support project export/import without exporting plaintext credentials. **P1**
- [ ] Specify tenancy isolation and test that every API query and mutation is scoped to the selected organization/project. **P0**

## 2. User identity and SSO connections

- [ ] Add named local accounts with password hashing, recovery, lockout/rate limiting, session expiry, and session revocation. **P0**
- [ ] Add OIDC SSO connection setup for Entra ID, Okta, Keycloak, and generic OIDC providers; test discovery, redirect, callback, logout, issuer, audience, nonce, state, and PKCE validation. **P0**
- [ ] Use maintained OIDC/OAuth libraries and cryptographic password hashing; test token and session failure paths without implementing protocol validation in Aušra code. **P0**
- [ ] Map stable IdP subject IDs to Aušra users and map IdP groups to Aušra roles; test removed users/groups lose access promptly. **P0**
- [ ] Support multiple SSO providers per organization, controlled JIT user creation, and an emergency local admin path. **P1**
- [ ] Add SAML 2.0 only if customers require it; test signed assertions, audience, clock skew, logout, and certificate rotation. **P1**
- [ ] Add SCIM provisioning/deprovisioning, including group synchronization and disabled-user session revocation. **P1**
- [ ] Add service accounts and scoped, expiring API tokens with rotation and last-used metadata. **P0**
- [ ] Enforce MFA through the IdP and expose the authentication context to sensitive-action policies where available. **P1**

## 3. Aušra roles, permissions, and approval separation

- [ ] Define platform admin, organization admin, project admin, author, operator, approver, and viewer roles with an explicit permission matrix. **P0**
- [ ] Enforce permissions on every API route and background action; verify direct API calls cannot bypass UI restrictions. **P0**
- [ ] Scope visibility and execution rights by organization, project, environment, connection, credential, and job. **P0**
- [ ] Separate permission to edit automation, request a run, approve it, and manage credentials. **P0**
- [ ] Prevent a requester from approving their own production run when separation of duties is enabled. **P0**
- [ ] Support time-limited elevated access with reason, approver, expiry, and audit trail. **P1**
- [ ] Test denied cross-project access, stale group membership, revoked tokens, and privilege changes during an active session. **P0**

## 4. Secret storage and credential lifecycle

- [ ] Create credential types for SSH keys, passwords, Ansible Vault, Git, AWS, Azure, GCP, Kubernetes, webhooks, and generic environment variables. **P0**
- [ ] Encrypt secret values at rest using a managed key or external KMS; keep key material outside the database and define rotation/recovery. **P0**
- [ ] Support Vault, AWS Secrets Manager, Azure Key Vault, and Google Secret Manager references without copying long-lived values into Aušra when possible. **P1**
- [ ] Scope credential use to authorized projects, environments, jobs, and runners; never return secret values from read APIs. **P0**
- [ ] Inject secrets only for the duration of a run and remove them from child process environments, temp files, and runner memory afterward. **P0**
- [ ] Redact known secret values and common credential formats from logs, events, errors, and support bundles; test failures and partial matches. **P0**
- [ ] Track credential owner, source, expiry, rotation, last use, and failed validation. **P1**
- [ ] Provide a connection test that checks permissions without displaying or persisting the underlying secret. **P0**

## 5. Cloud credentials and account connections

- [ ] Model a cloud connection as provider, account/subscription/project, region, auth method, permitted roles, network route, and health state. **P0**
- [ ] Prefer short-lived workload identities; make static access keys an explicit fallback with rotation and expiry alerts. **P0**
- [ ] AWS: support instance/task roles, EKS Pod Identity or IRSA when Aušra runs on EKS, STS AssumeRole for target accounts, external ID where applicable, session tags, and regional STS. **P0**
- [ ] Use official provider SDKs for credential discovery, refresh, role assumption, API calls, and error classification; avoid custom signed requests or credential protocol code. **P0**
- [ ] AWS: document and validate each connection's role trust policy, permission policy, permission boundary, and applicable organization SCP; verify the effective role has only the required actions and resources. **P0**
- [ ] AWS: support an off-AWS identity option such as IAM Roles Anywhere or an approved OIDC federation path; test credential refresh and cross-account role trust. **P1**
- [ ] AWS: validate `GetCallerIdentity`, target account ID, assumed role ARN, allowed regions, and representative read/write permissions before saving a connection. **P0**
- [ ] Azure: support managed identity on Azure and workload identity federation for external runners; capture tenant, subscription, client identity, and allowed scopes. **P1**
- [ ] Azure: test token refresh, subscription discovery, role assignments, and a permission-denied path without a client secret. **P1**
- [ ] GCP: support attached service accounts and Workload Identity Federation; capture organization/folder/project scope and service account impersonation where used. **P1**
- [ ] GCP: test project access, token refresh, and denial outside the granted IAM scope without a service-account key. **P1**
- [ ] Ensure each job can select only a connection its caller and project may use; record the cloud principal and target account in the run audit trail. **P0**
- [ ] Detect expired or broken trust, surface actionable connection errors, and avoid silently falling back to a broader host identity. **P0**

## 6. Infrastructure and network connections

- [ ] Add managed SSH connections with host-key verification, bastion/proxy support, per-environment keys, and a reachability test. **P0**
- [ ] Add Git connections via deploy keys, app installations, or scoped tokens; enforce host verification and repository allowlists. **P0**
- [ ] Add HTTP/API connections with TLS validation, custom CA bundles, proxy settings, and bounded timeouts. **P1**
- [ ] Add WinRM/PowerShell connections if Windows targets are in scope; test TLS and least-privilege remote execution. **P1**
- [ ] Show network path and DNS errors separately from authentication and authorization failures. **P0**
- [ ] Use egress allowlists and runner placement so private infrastructure is reachable without exposing inbound management ports. **P1**

## 7. EKS connection and Kubernetes permissions

**Two distinct connections are needed:** Aušra's own AWS identity when it runs in EKS, and the identity Aušra uses to access each target cluster. Kubernetes permissions are another layer after AWS authentication.

- [ ] Connect a target EKS cluster by AWS account, region, cluster name, endpoint, CA data, and chosen execution role; validate `DescribeCluster`. **P0**
- [ ] Use AWS SDK for EKS discovery and a Kubernetes client library for API operations; verify token refresh, TLS, namespace scope, and denied actions. **P0**
- [ ] Use EKS access entries/access policies for the target IAM role where supported; map that role to the minimum Kubernetes RBAC required for each action. **P0**
- [ ] Verify the target role's AWS policy allows cluster discovery/authentication while its EKS access entry and Kubernetes Role/RoleBinding grant only the intended namespace actions. **P0**
- [ ] Test Kubernetes API authentication separately from Kubernetes authorization, including namespace-scoped read, deploy, and denied cluster-admin operations. **P0**
- [ ] Support private EKS API endpoints through a runner in the VPC or connected network; verify DNS, routing, security groups, and CA validation. **P0**
- [ ] Generate short-lived EKS authentication tokens per run; avoid storing kubeconfig bearer tokens. **P0**
- [ ] Support cluster/namespace selection, context pinning, and an explicit production target confirmation. **P0**
- [ ] When Aušra runs *inside* EKS, bind its Kubernetes service account to a limited AWS IAM role using EKS Pod Identity or IRSA; block use of the node role as an implicit fallback. **P0**
- [ ] Use separate IAM roles for Aušra control plane, runners, and target-cluster operations; test cross-account role chaining. **P0**
- [ ] Add Kubernetes job actions: inspect resources, diff/apply manifests, Helm deploy/rollback, wait for rollout, and read diagnostics. **P1**
- [ ] Record cluster ARN, namespace, Kubernetes service account/IAM principal, change set, and outcome for every cluster run. **P0**
- [ ] Test cluster credential rotation, endpoint change, access-entry removal, RBAC denial, API outage, and runner restart. **P1**

## 8. Source control, inventories, and execution

- [x] Connect public HTTPS Git repositories, choose a default branch/branch/tag/full commit, sync on demand, use a repository as a job source, and record the pinned commit for Git-backed runs. **P0** Verify a later sync cannot change an already requested run.
- [ ] Add private Git connections through scoped credentials, repository allowlists, and host verification without placing secrets in URLs. **P0**
- [ ] Verify Git webhook signatures, prevent replay, and trigger jobs from allowed events. **P0** GitHub SHA-256 verification, ref filtering, opt-in job/workflow mapping, approval-gated runs, exact `after` commit matching, and a durable delivery-ID ledger are implemented. Tests cover replay after recent-history eviction and restart. On upgrade, IDs already evicted from the old JSON history cannot be backfilled; rotate the webhook secret in GitHub to invalidate old signed deliveries before calling replay protection complete.
- [ ] Treat repositories and user-supplied job definitions as untrusted code; run them in isolated, disposable worker environments. **P0**
- [ ] Add Ansible inventories from files, cloud inventory plugins, and typed variables; protect Vault material and show dry-run/check-mode output. **P1**
- [ ] Add Terraform/OpenTofu `init`, `validate`, `plan`, `apply`, and `destroy` flows with separate permissions for destructive actions. **P0**
- [ ] Persist immutable plan artifacts and require apply to use the exact approved plan/revision; detect a changed plan or stale approval. **P0**
- [ ] Support remote state backends, state locking, workspaces, provider caches, and module registries without storing state secrets in run logs. **P0**
- [ ] Pin tool versions and execution images per job; capture versions in the run record. **P1**
- [ ] Add queued runs, concurrency limits per project/environment/target, timeouts, retries, cancellation, and fair scheduling. **P0**
- [ ] Stream logs with timestamps and step attribution; bound retention and sanitize terminal control characters. **P1**

## 9. Workflows, approvals, and policy gates

- [ ] Add parallel steps, conditional branches, reusable workflows, failure handlers, retries, and parameter passing with typed inputs. **P1**
- [ ] Add approvals at specific steps, named approver groups, expiry, rejection, delegation, and immutable approval context. **P0**
- [ ] Add policy-as-code checks for plan content, allowed regions/resources, tags, environment restrictions, and destructive operations. **P1**
- [ ] Define policy outcomes (`allow`, `deny`, `require approval`) and version the policy used for each decision. **P1**
- [ ] Add drift detection, scheduled scans, comparison to desired state, and approved remediation. **P2**
- [ ] Add cost estimation to plan review where provider data supports it; label estimates and unknowns clearly. **P2**
- [ ] Provide self-service forms with validated fields and predefined jobs, without giving requesters edit rights to scripts. **P1**

## 10. Triggers and integrations

- [ ] Support cron schedules with time zones, missed-run policy, daylight-saving behavior, and maintenance windows. **P1**
- [ ] Add authenticated webhooks with signature verification, replay protection, rate limits, and idempotency keys. **P1** GitHub per-repository secrets, signature validation, a durable delivery-ID ledger, and per-repository rate limits are implemented; other providers remain.
- [ ] Add Git, alert, and incident triggers with mapping from event to project/job and a test-event facility. **P1** GitHub push mapping and an approval-waiting test-run facility are implemented; alert and incident sources remain.
- [ ] Add notification routes for run start/failure, approval requests, policy denies, and drift; allow project-level configuration. **P1**
- [ ] Publish a stable API, CLI, and event schema for external integrations. **P1**

## 11. Audit, monitoring, and operations

- [ ] Record actor, action, target, before/after metadata, request/run ID, timestamp, IP, cloud principal, and policy decision in tamper-evident audit events. **P0**
- [ ] Keep secret material and sensitive plan values out of audit records; provide configurable retention and export. **P0**
- [ ] Add health/readiness endpoints, metrics, tracing, structured logs, alerts, and runner heartbeat monitoring. **P0**
- [ ] Back up PostgreSQL, encryption keys, and artifacts; perform a documented restore test. **P0**
- [ ] Provide HA control-plane deployment, graceful upgrades, schema compatibility checks, and disaster recovery objectives. **P2**
- [ ] Publish Docker/Kubernetes deployment guides, resource sizing, ingress/TLS examples, and secure configuration defaults. **P1**

## 12. Release gates and acceptance scenarios

- [ ] Run unit, integration, race, API authorization, and browser tests in CI; scan dependencies and container images. **P0**
- [ ] Test with a real SSO provider: user signs in, inherits a project role from a group, loses access when removed, and cannot self-approve. **P0**
- [ ] Test an AWS cross-account connection: Aušra assumes only the configured role and cannot reach an unrelated account. **P0**
- [ ] Test EKS end to end: private endpoint runner connects, namespace-limited action succeeds, cluster-wide action is denied, and audit identifies the principal. **P0**
- [ ] Test Terraform/OpenTofu: plan is reviewed, approved, applied unchanged, then rejected when source or plan differs. **P0**
- [ ] Test secret rotation during and between runs; verify no secret appears in API responses, logs, or database exports. **P0**
- [ ] Test runner crash, controller restart, database outage, duplicate webhook, timeout, cancellation, and retry without duplicate infrastructure changes. **P0**
- [ ] Perform a threat review and a production pilot with least-privilege roles, backup restore, and rollback before calling Aušra team-ready. **P0**

## Design references

- [AWS IAM guidance for temporary workload credentials](https://docs.aws.amazon.com/IAM/latest/UserGuide/best-practices.html) and [IAM Roles Anywhere for off-AWS workloads](https://docs.aws.amazon.com/rolesanywhere/latest/userguide/introduction.html).
- [EKS access entries for cluster API users and roles](https://docs.aws.amazon.com/eks/latest/userguide/access-entries.html), [EKS Pod Identity for pods](https://docs.aws.amazon.com/eks/latest/userguide/pod-identities.html), and [EKS API endpoint modes](https://docs.aws.amazon.com/eks/latest/userguide/cluster-endpoint.html).
- [Microsoft Entra authorization code flow with PKCE](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow) and [Azure workload identity federation](https://learn.microsoft.com/en-us/entra/workload-id/workload-identity-federation).
- [Google Cloud workload identity options](https://docs.cloud.google.com/iam/docs/workload-identities) and [Workload Identity Federation](https://docs.cloud.google.com/iam/docs/workload-identity-federation).
- [WCAG 2.2](https://www.w3.org/TR/WCAG22/), [W3C accessible widget practices](https://www.w3.org/WAI/ARIA/apg/practices/), and [reduced-motion guidance](https://www.w3.org/WAI/WCAG22/Understanding/animation-from-interactions).
