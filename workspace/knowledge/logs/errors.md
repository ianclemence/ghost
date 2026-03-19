---
type: error-log
created: 2026-03-19
updated: 2026-03-19
tags: [logs, errors]
description: Chronological error log. Deduplicated against self/recent-errors.
---

# Error Log

Timestamp-ordered error records. Newest entries at top.

## 2026-03-19 — doctor.New signature mismatch

- **Time**: ~14:00
- **What**: Build failure after adding workspace parameter to `doctor.New`
- **Symptom**: `not enough arguments to return` in agent, commands, and doctor packages
- **Root cause**: Added `workspace string` param to `Doctor` struct without updating all call sites
- **Resolution**: Updated 3 call sites (agent/loop.go, builtin_doctor_test.go, doctor_test.go)
- **See**: [[self/recent-errors]]

## 2026-03-19 — speedtest SKILL.md malformed frontmatter

- **Time**: ~13:00
- **What**: SKILL.md had `#---` on first line (invalid YAML anchor)
- **Symptom**: Frontmatter parser returned empty; prerequisites not loaded
- **Resolution**: Removed `#---` line; fixed to clean `---` document marker
- **See**: [[self/recent-errors]]

## 2026-03-19 — skills dependency check returning empty

- **Time**: ~13:00
- **What**: `CheckSkillDependencies` returned empty results for all skills
- **Symptom**: Skills showed no prerequisites even when commands were listed
- **Root cause**: YAML frontmatter `prerequisites: commands:` vs flat JSON structure; fixed via `parsePrerequisitesFromYAML`
- **Resolution**: Regex extraction of `prerequisites:\n  commands:\n    - cmd` path
- **See**: [[self/recent-errors]]
