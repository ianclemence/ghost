# Third-Party Notices

## Needle (Cactus Compute, Inc.) — Apache License 2.0

Ghost's Sting offline tool-router is fork-compatible with, and its
initial engine underneath is, **Needle 2** by Cactus Compute, Inc.:

- Source: https://github.com/cactus-compute/needle
- Weights: https://huggingface.co/Cactus-Compute/needle2
- License: Apache License, Version 2.0 (http://www.apache.org/licenses/)
- Paper: "Needle 2: A 45M-Parameter Foundation Tool-Calling Model for
  Tiny Devices" (arXiv:2607.18363)

```
Copyright (c) Cactus Compute, Inc.
```

What Ghost uses under this license:

- The `cactus-needle` Python runtime and prebuilt engine libraries
  (fetched once and cached; inference itself is offline).
- The base `needle2` model weights (Apache-2.0 per the HuggingFace
  model page) for the default sidecar engine.
- The vendored tokenizer (`sting-sidecar/vendor/tokenizer.model` +
  `tokenizer.vocab`, from `cactus-needle==2.0.15`) pinned for
  reproducible dataset measurement.
- The sidecar protocol shape (tool JSON schemas, the
  `{type, function_calls, reasoning, confidence}` envelope) and the
  fine-tuning data format, so Ghost-tuned weights stay portable.

What Ghost changed / owns:

- All Go code (`pkg/sting`, provider adapter, agent fast-path,
  confidence gate, grounding validation) is an independent
  implementation under Ghost's MIT license.
- The sidecar daemon, systemd unit, and install flow are Ghost's.
- "Sting" and "Ghost" are Ghost's names; **"Needle" is a trademark of
  Cactus Compute, Inc.** and is used here only to identify the
  upstream project, as the Apache-2.0 license requires attribution but
  grants no trademark rights.

If you distribute Ghost with the Needle engine or weights bundled, keep
this notice and the upstream `LICENSE` terms for those artifacts.
