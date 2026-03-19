---
type: errors
created: 2026-03-19
updated: 2026-03-19
tags: [self, errors, debugging]
description: Recent errors, their causes, and resolutions.
---

# Recent Errors

Log significant errors here. Include: what happened, root cause, what resolved it.

## 2026-03-19 — doctor.New signature mismatch

- **What**: Build failure after adding workspace parameter to `doctor.New`
- **Symptom**: `not enough arguments to return` across agent, commands, and doctor test files
- **Root cause**: Added `workspace string` param to `Doctor` struct and constructor without updating all call sites
- **Resolution**: Updated all 3 call sites:
  - `pkg/agent/loop.go`: added workspace argument
  - `pkg/commands/builtin_doctor_test.go`: added `t.TempDir()`
  - `pkg/doctor/doctor_test.go`: added `t.TempDir()` and updated expected check count from 5→6
- **Files affected**: `pkg/doctor/doctor.go`, `pkg/agent/loop.go`, `pkg/commands/builtin_doctor_test.go`, `pkg/doctor/doctor_test.go`

## 2026-03-19 — skills dependency check returning empty

- **What**: `CheckSkillDependencies` returned empty results for all skills
- **Symptom**: All skills showed as having no prerequisites
- **Root cause**: Frontmatter YAML block was using `prerequisites:` as parent key with `commands:` as nested key, but the JSON unmarshal path expected flat `{"prerequisites": {"commands": [...]}}` structure
- **Resolution**: `parsePrerequisitesFromYAML` regex extracts `prerequisites:\n  commands:\n    - cmd1` correctly; confirmed working against live skill frontmatter
- **Files affected**: `pkg/skills/dependencies.go`

## 2026-03-19 — speedtest skill malformed frontmatter

- **What**: SKILL.md had `#---` on first line (invalid YAML anchor before document start)
- **Symptom**: Frontmatter parser returned empty; prerequisites not loaded
- **Resolution**: Fixed by removing the `#---` line; clean `---` document marker on line 1
- **Files affected**: `workspace/skills/speedtest/SKILL.md`

## 2026-03-19 — skill count mismatch: 28 vs 33

- **What**: Ghost via Telegram/mobile reported 28 skills; `ghost skills list` CLI reported 33
- **Symptom**: Agent gave inaccurate skill count depending on how it was invoked
- **Root cause**: `ContextBuilder.NewContextBuilder()` used `filepath.Join(wd, "skills")` for builtin skills, where `wd` is the CWD of the running ghost binary. This resolved to the ghost project's own `skills/` directory (5 extra skills), not `<globalConfigDir>/ghost/skills` which is where the CLI looks. The agent loop used `~/.GHOST/skills` for global skills, matching the CLI, but the builtin path was wrong.
- **Resolution**: Changed `builtinSkillsDir` from `filepath.Join(wd, "skills")` to `filepath.Join(getGlobalConfigDir(), "ghost", "skills")`, matching the CLI's builtin skills path. Both global and builtin paths now use the global config directory.
- **Files affected**: `pkg/agent/context.go`

## Template for new entries

```
## YYYY-MM-DD — Short description

- **What**: One-line description
- **Symptom**: What was observed
- **Root cause**: Why it happened
- **Resolution**: How it was fixed
- **Files affected**: file paths
```
