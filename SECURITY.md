# Security policy

This SDK is in alpha and has not completed a production security review. It validates bounded
discovery messages but does not authenticate providers, enforce network egress policy, or sandbox
plugins. Use Core's supervised process protocol only for trusted local plugins, and validate
resolved destinations against the application's egress policy before connecting.

Report suspected vulnerabilities privately to the repository maintainers. Do not include usable
credentials, personal data, or private endpoint details in a public issue. Maintainers should
acknowledge reports, coordinate a fix, and publish an advisory after affected users can update.

## Reporting limits

This package is alpha. No stable-release or response-time commitment is implied by this policy. Do not include credentials, personal data, private keys, or production endpoints in a report.
