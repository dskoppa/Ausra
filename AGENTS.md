# Aušra development guidance

- Prefer maintained libraries and official SDKs over custom implementations for standard capabilities. Keep custom code focused on Aušra's product behavior and visual identity.
- Before implementing a checkpoint, consult `LIBRARIES.md`; choose an existing library when it fits, and record a short reason in the change if bespoke code is necessary.
- Never hand-roll cryptography, password hashing, OIDC/OAuth/SAML validation, cloud credential signing, Kubernetes authentication, or secret-store protocols. Use established libraries and provider SDKs.
- Add dependencies when their feature is implemented, not merely planned. Pin versions, review maintenance and license fit, and test the integration and failure paths.
- Preserve the working Docker self-hosted prototype during incremental migrations. Do not claim a planned library is installed or a checkpoint is complete until behavior is implemented and verified.
