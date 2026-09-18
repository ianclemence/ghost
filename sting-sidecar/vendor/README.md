# Vendored tokenizer (Sting independence row 2)

`tokenizer.model` (132KB) + `tokenizer.vocab` (112KB), copied from
`cactus-needle==2.0.15` (Cactus Compute, Apache-2.0 — see
`THIRD-PARTY-NOTICES.md`).

Why vendored: the flywheel measures rendered dataset rows against this
exact tokenizer (rows over `--max-len` are silently truncated =
corrupted labels). Pinning it in-repo means dataset validation is
reproducible without PyPI/HF access — one fewer network dependency on
the way to full independence. Training/finetuning still uses the
installed package's copy; this pair is the reference for measurement.

Do NOT edit these files. To re-vendor after an upstream bump, copy both
files from the installed `needle/model/` directory and record the
version below.

- Vendored from: cactus-needle 2.0.15 (2026-09-17)
- Upstream: https://github.com/cactus-compute/needle
