# Responsible use

ghemails is intended for asset discovery and exposure review during an authorized security assessment. A public email address is personal information and is not an invitation to contact its owner.

## Operator requirements

- Obtain written authorization that covers the target, GitHub scope, collection method, and assessment window.
- Use `--visibility public` unless the authorization explicitly covers private or internal repositories accessible to your token.
- Minimize collection: choose fast or balanced mode when deep history is unnecessary and set appropriate repository/commit/history limits.
- Store reports and caches with access controls appropriate to the engagement, transmit them securely, and delete them when retention ends.
- Validate findings manually before including them in a report. An observed address may be old, aliased, intentionally published, or unrelated to a security impact.
- Honor complaints, removal requests, do-not-contact requests, laws, program rules, and GitHub's current terms.

## Prohibited uses

Do not use ghemails for spam, phishing, harassment, doxxing, credential attacks, unsolicited recruiting, sale of personal data, rate-limit evasion, or access outside the authorized scope.

The tool performs no SMTP mailbox probing and generates no guessed addresses. Deep mode clones Git repositories but does not execute repository code, hooks, submodules, or local Git configuration.
