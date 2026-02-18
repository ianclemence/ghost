---
type: reference
tags: [architecture, core]
created: 2026-02-18
source: docs/GHOST_OS.md
description: Overview of the Ghost Operating System architecture and core concepts.
---

# Ghost OS

> **Note**: This is an ingested reference document.

## Overview
Ghost is a distributed operating system for autonomous AI agents. It enables multiple specialized agents to run simultaneously while maintaining strict isolation between tenants and users.

## Core Concepts
- **Agent**: A specialized AI worker with a role, skills, memory, and autonomy level.
- **Tenant**: An isolated environment with its own agents, storage, and subdomain.

## System Architecture
The system consists of a **Control Plane** (API Gateway, Scheduler) managing multiple **Tenants**.
Each tenant has:
- Multiple Agents (Sales, Support, Exec, etc.)
- Dedicated Skills (CRM, Email, Calendar)
- Isolated Memory (SQLite + RAG)
- Local LLM (Ollama) or Cloud LLM (Kimi)

## Autonomy Levels
1.  **Reactive**: Waits for user message (e.g., FAQ chatbot).
2.  **Scheduled**: Wakes at intervals (e.g., Daily briefing).
3.  **Event-Driven**: Listens to webhooks (e.g., New lead -> Qualify).
4.  **Fully Autonomous**: Sets own goals (e.g., Research agent).

## Multi-Tenant Isolation
- **Filesystem**: Dedicated directories.
- **Network**: Subdomain routing.
- **Memory**: Separate databases.
- **Compute**: Container/process isolation.

## Deployment
- **Personal**: Raspberry Pi 5 (2-3 agents).
- **Small Business**: Pi Cluster (HAProxy).
- **SaaS**: Container orchestration (Kubernetes/Docker Swarm).
