---
type: channels
created: 2026-03-19
updated: 2026-03-19
tags: [self, channels, integrations]
description: Status of configured messaging channels and integrations.
---

# Channels

Status of all configured messaging channels. Unknown values indicate not-yet-verified configuration.

## Telegram

- **Status**: Unknown
- **Bot token**: Not verified
- **Configured**: Unknown
- **Notes**: Primary channel for Ghost if configured

## Discord

- **Status**: Unknown
- **Guild ID**: Unknown
- **Bot token**: Not verified
- **Configured**: Unknown

## Slack

- **Status**: Unknown
- **Workspace**: Unknown
- **Bot token**: Not verified
- **Configured**: Unknown

## WhatsApp

- **Status**: Unknown
- **Configured**: Unknown

## Email

- **Provider**: himalaya (see [[skill:email/himalaya]])
- **Status**: Unknown
- **Accounts**: Unknown
- **Configured**: Unknown

## LINE

- **Status**: Unknown
- **Configured**: Unknown

## Other Channels

Check `pkg/channels/` for all registered channel implementations.

## How to Update

Run channel health checks and update this file with results. Mark as `active`, `inactive`, or `not-configured`.

## Last Verified

_(Update with date when verified)_
