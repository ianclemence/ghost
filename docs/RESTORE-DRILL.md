# Restore drill

A backup nobody has restored is a rumor. This checklist proves recovery
works with nothing but the archive and the vault master key. Run it
quarterly, and after every change to export/import, retention, or the
vault.

## What you need

- One `.gst` archive from `/var/lib/ghost/backups/` (weekly timer or
  `ghost state backup`; pre-update snapshots land here automatically).
- The vault master key (`GHOST_MASTER_KEY`, from `/var/ghost/config/.master-env`).
- Nothing else. Not the database, not the config, not the old disk.

## The drill (fresh box or wiped install)

1. Install Ghost to the point of a bootable tree (binaries + systemd
   units, no state).
2. `sudo systemctl stop ghost ghost-web` — restore never runs under a
   live gateway.
3. Copy the archive onto the box. Verify it first without extracting:
   `ghost state inspect <archive>` (prompts for the passphrase; use
   the master key). Confirm the Ghost ID and file count look right.
4. `ghost state import <archive> --force` with the master key as the
   passphrase. Expect: identity restored, conversations and memory
   rehydrated, secrets re-sealed to this machine's key.
5. `sudo systemctl start ghost` and watch the boot log: schema
   migration, then `Database integrity ok`. Any integrity failure
   refuses boot with the same restore instruction — that refusal is
   the safety working, not a second problem.
6. `ghost status` — health ok, workspace present, model correct.
   `ghost state prune` state sane; spot-check one memory and one
   routine from before the wipe.

## Pass criteria

- [ ] Inspect succeeds with master key only.
- [ ] Import completes on a fresh target.
- [ ] Boot passes migration + integrity gate.
- [ ] Status is healthy; spot checks match.
- [ ] The whole drill used nothing but the archive and the key.

## If it fails

Record which step failed and file it as a P0 against backup/restore:
a drill failure is a backup failure. Do not "fix forward" by copying
live state over the fresh box — that tests nothing.
