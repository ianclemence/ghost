---
type: reference
tags: [concept, skills, architecture]
created: 2026-02-18
source: docs/SKILL_GRAPHS.md
---

# Skill Graphs

> **Note**: This is an ingested reference document.

## Core Concept

People underestimate the power of structured knowledge. It enables entirely new kinds of applications.

Right now, people write skills that capture one aspect of something (e.g., summarizing, code review). Often, this is just one file with one capability. That's fine for simple tasks, but real depth requires something else.

**Skill Graphs** are networks of skill files connected with `[[wikilinks]]`. Instead of one big file, you have many small, composable pieces that reference each other. Each file is one complete thought, technique, or skill, and the links between them create a traversable graph.

## Primitives

1.  **Wikilinks**: woven into prose (`[[wikilinks]]`) so they carry meaning, not just references.
2.  **YAML Frontmatter**: descriptions the agent can scan without reading the whole file.
3.  **MOCs (Maps of Content)**: organize clusters of related skills into navigable sub-topics.

## Progressive Disclosure

Traversal follows a pattern of progressive disclosure:
`Index -> Descriptions -> Links -> Sections -> Full Content`

Most decisions happen _before_ reading a single full file.

## Use Cases

- **Trading Skill Graph**: risk management, market psychology, technical analysis.
- **Legal Skill Graph**: contract patterns, compliance, jurisdiction specifics.
- **Company Skill Graph**: org structure, product knowledge, processes.

## Ars Contexta

Ars Contexta is a skill graph that teaches an agent how to build skill graphs (specifically knowledge bases). It consists of ~250 connected markdown files that the agent traverses to derive a local knowledge system.
