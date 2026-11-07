# Server handoff

This release is prepared without changing the running CPA installation.
Do not restart a CPA/New API service that supplies the current agent session.

1. Confirm current CPA health and persistent mounts.
2. Back up the old plugin's state from inside the running container:
   `/CLIProxyAPI/codex-auto-reset.state.json`. Save it outside the container.
3. Record the old plugin's enabled accounts and current unresolved reset state.
4. Verify the new Release ZIP against `checksums.txt`; confirm the root entry
   is `cpa-codex-reset-manager.so` for Linux.
5. Install through CPA's supported plugin installation flow, using
   `data_dir: /CLIProxyAPI/data/cpa-codex-reset-manager`.
6. Start with monitoring only. Confirm settings survive a plugin reload and the
   two files are present on the host's mounted data directory.
7. Disable the original plugin's automatic redemption. Resolve any outstanding
   old-plugin redemption before enabling the new plugin's reset policies.
8. Confirm quota samples, notices, settings revisions and activity records.

If a host restart is required, arrange it from an independent control session.
New API does not need any restart. Rollback consists of disabling the new reset
policies and restoring the old plugin configuration after confirming that no
new redemption is pending. Preserve both plugins' histories for reconciliation.

## Subsequent hot updates

Install a verified, versioned library filename and toggle only this plugin off
and back on through CPA's plugin-management API. Saving an already-true enabled
flag may not activate a newly placed file. The off/on transition drains this
plugin before loading the new version; CPA and New API do not need a restart.

Back up settings.json and runtime.json before updating. Once notification
channels have been configured, also back up notifications.json and
notification-log.json. Keep credentials private. Check that reset policy bytes
are unchanged and that the notification channels remain in their prior state.
Do not perform real redemptions or send real test messages during deployment.
