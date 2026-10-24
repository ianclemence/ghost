# System design review

A first pass over how a request becomes an action, found by following one real
failure end to end: an owner asks Ghost to clone a repository and it cannot.
Everything below was reproduced against the code, and each fix has a test that
fails on the old behaviour. This is not a full audit; the last section lists
what has not been looked at.

## How a tool reaches the model

```
message ─► FilterToolsForTurn ─► offered tools ─► model ─► tool call
              │  profile allowlist                           │
              │  keyword gates (13 languages)                ▼
              │  intent routes                      permission broker (grant)
              ▼                                              │
        hidden primitives (exec, sandbox)                    ▼
          unhidden by: command intent,             ExecTool ─► sandbox (bwrap)
          code-work intent, find_tools                      ├─ workspace: read-only
                                                            ├─ tmp, knowledge, data, learning: writable
                                                            └─ projects/<name>: writable
```

Visibility (what the model is offered) and authority (what may run) are separate
layers. Keyword gates and `find_tools` only widen visibility; the broker and the
sandbox decide what runs. That separation is sound and was kept.

## Findings

| # | Sev | Finding | Evidence | Fix |
|---|---|---|---|---|
| 1 | P1 | A command could run with Ghost's own config directory as its working directory, read its files and write to it. The sandbox binds a working directory outside the workspace read-write, and only a short list of runtime folders was refused. | Test ran `cat secret.key; echo x > planted` in a sibling `config/`: output `TOPSECRET`, file written. | `confineWorkingDir`: a command runs in the workspace or in a real folder under `projects/`, judged by where the path leads, so a link cannot lead out. |
| 2 | P1 | The tool registry wrote to a map under a read lock (`isVisible` deleted expired hides) and `isAllowed` and `IsHidden` read and wrote it with no lock. Two concurrent readers after a hide expires crash the process. | `fatal error: concurrent map writes` and `concurrent map read and map write` from the new tests on the old code. | Visibility is a pure read; internal `...Locked` helpers assume the lock; public methods take it. |
| 3 | P1 | A repository could not be cloned. `exec` was hidden until a message said "run ...", and the catalogue and `find_tools` skipped hidden tools, so there was no route to it in any language. | Live request got "the command tool isn't available". | Language-neutral code-work intent, `exec` listed in the catalogue, `find_tools` can promote hidden primitives through the same profile and policy check. |
| 4 | P1 | Nothing created project folders, and the sandbox only binds a working directory that exists, so `projects/` could never be written. The first fix relied on the model naming `working_dir`; the live model did not. | Live log: `cd projects && git clone ...` from the default directory, "Read-only file system". | The runtime mounts the real `projects/` at `workspace/projects`, so the natural path works; inside one project only that project is mounted. A named `projects/<name>` is created under a validated name, never through a link. |
| 5 | P2 | A relative `working_dir` was checked against the workspace but run relative to the process. | Read of `Execute` and `guardCommand`. | Resolved once to an absolute path. |
| 6 | P2 | Keyword tool gates were English-only, so a tool the words clearly asked for was not offered in other languages. | Per-language test cases. | Words for 13 languages merged into the existing gates; the catalogue and `find_tools` remain the language-neutral fallback. |

## Decisions

- **Make the natural path work instead of instructing the model.** A fix that
  depends on the model passing the right parameter failed in the first live
  test. The runtime now owns where projects live and how they are mounted.
- **Mount the projects root only outside a project.** Least privilege for work
  inside a project; usability for creating one. The cost is that a command with
  no project directory sees every project. They are one owner's projects on one
  device.
- **Keep keyword gates, add the fallback.** They save tokens and help weaker
  models. The language-neutral path is the catalogue plus `find_tools`; a
  model-chooses-from-a-list design was considered and left for later because it
  costs tokens on every turn and the local model has not been tested on it.

## Not yet examined

Provider adapters (streaming, retry, cancellation), memory provenance and
promotion of inferred facts, prompt-injection handling of tool results,
proactive-proposal lifecycle and verification, API and mobile event contracts,
crash recovery of scheduled work, and resource use on the 8 GB Pod. The race
detector is unavailable on this kernel (`ThreadSanitizer: unsupported VMA
range`), so races were found by tests that provoke the runtime's own map checks;
other races may remain.
