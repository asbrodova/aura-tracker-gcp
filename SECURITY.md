# Security Policy

## Supported Versions

Aura Tracker GCP is an actively evolving open-source project. Security fixes are provided for the latest published release.

| Version | Supported |
|---|---|
| Latest release | Yes |
| Older releases | No |

Before reporting a vulnerability, please confirm that it is reproducible with the latest release.

## Reporting a Vulnerability

Please do **not** open a public GitHub issue for a suspected security vulnerability.

Report it privately through [GitHub's security advisory form](https://github.com/asbrodova/aura-tracker-gcp/security/advisories/new).

Please include, where possible:

- The affected Aura version or commit
- Whether Aura is running through stdio or remote SSE
- A description of the vulnerability and its potential impact
- Minimal reproduction steps or a proof of concept
- Relevant configuration with credentials and project identifiers removed
- Suggested remediation, if known

Never include service-account keys, access tokens, secret values, billing data, private logs, or sensitive GCP project information.

## Security-Sensitive Areas

Examples of issues that should be reported privately include:

- Authentication or authorization bypass in remote SSE deployments
- Exposure of credentials, tokens, secrets, billing data, or project identifiers
- Cross-environment data leakage between configured project aliases
- Bypassing preview-and-confirm safeguards for infrastructure mutations
- Injection through tool inputs, outputs, prompts, diagrams, or rendered SVG
- Access to Secret Manager secret values
- Commands or GCP operations executed outside the documented scope
- Vulnerabilities that could cause unauthorized GCP changes or unexpected cost

General configuration questions, non-security bugs, and insecure GCP IAM policies that Aura only reports should use the [public issue tracker](https://github.com/asbrodova/aura-tracker-gcp/issues).

## What to Expect

Security reports are reviewed on a best-effort basis. Response times may vary.

When a report is confirmed, the maintainer will aim to:

1. Assess the impact and affected versions
2. Coordinate remediation with the reporter
3. Prepare a fix and regression coverage
4. Publish a security advisory and patched release when appropriate
5. Credit the reporter, unless anonymity is requested

Please allow reasonable time for investigation and remediation before public disclosure.

## Responsible Research

Good-faith security research is welcome. Please:

- Avoid accessing data that does not belong to you
- Test only against GCP projects and environments you are authorized to use
- Avoid service disruption, destructive operations, and unnecessary cloud cost
- Collect only the minimum evidence required to demonstrate the issue
- Do not retain or disclose sensitive data

This project does not currently operate a paid bug-bounty program, and rewards cannot be guaranteed.
