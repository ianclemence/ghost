# Ghost OS: Multi-Agent & Multi-Tenant Architecture

> "From a single presence to a collective of specialized intelligences."

## Overview

Ghost is evolving from a personal AI companion into a distributed operating system for autonomous AI agents. This architecture enables multiple specialized agents to run simultaneously—each with distinct skills, memory, and autonomy levels—while maintaining strict isolation between tenants and users.

---

## Core Concepts

### What is a Ghost Agent?

An agent is a specialized AI worker with:
- **Role**: Specific purpose (sales, support, research, executive)
- **Skills**: Tools it can use (CRM, email, calendar, vision)
- **Memory**: Isolated SQLite database + RAG vector store
- **Autonomy**: How independently it operates (reactive to fully autonomous)
- **LLM Profile**: Model selection (local Ollama or cloud Kimi)

### What is a Tenant?

A tenant is an isolated environment containing:
- One or more agents
- Dedicated storage (filesystem, database, vector index)
- Unique subdomain (e.g., `acme.ghost.ai`)
- Independent configuration and billing

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Ghost Control Plane                      │
│                    (API Gateway + Scheduler)                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  Tenant Mgr │  │  Agent Mgr  │  │   Skill Registry    │  │
│  │ (acme, beta)│  │(spawn, kill)│  │ (register, route)   │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│  Tenant A    │      │  Tenant B    │      │  Tenant C    │
│  acme.ghost  │      │  beta.ghost  │      │  gamma.ghost │
│              │      │              │      │              │
│ ┌──────────┐ │      │ ┌──────────┐ │      │ ┌──────────┐ │
│ │ Agent A1 │ │      │ │ Agent B1 │ │      │ │ Agent C1 │ │
│ │ (Sales)  │ │      │ │ (Support)│ │      │ │ (Exec)   │ │
│ │ Skills:  │ │      │ │ Skills:  │ │      │ │ Skills:  │ │
│ │ - CRM    │ │      │ │ - Ticket │ │      │ │ - Email  │ │
│ │ - Email  │ │      │ │ - KB     │ │      │ │ - Cal    │ │
│ │ - Cal    │ │      │ │ - Escal  │ │      │ │ - Report │ │
│ └──────────┘ │      │ └──────────┘ │      │ └──────────┘ │
│              │      │              │      │              │
│ ┌──────────┐ │      │ ┌──────────┐ │      │              │
│ │ Agent A2 │ │      │ │ Agent B2 │ │      │              │
│ │(Research)│ │      │ │ (Onboard)│ │      │              │
│ └──────────┘ │      │ └──────────┘ │      │              │
│              │      │              │      │              │
│ SQLite + RAG │      │ SQLite + RAG │      │ SQLite + RAG │
│ Ollama local │      │ Ollama local │      │ Ollama local │
└──────────────┘      └──────────────┘      └──────────────┘
       │                      │                      │
       └──────────────────────┼──────────────────────┘
                              ▼
                    ┌─────────────────┐
                    │   Kimi K2.5     │
                    │  (Cloud Oracle) │
                    │  Complex tasks  │
                    └─────────────────┘
```

---

## Autonomy Levels

| Level | Behavior | Example Use Case |
|-------|----------|----------------|
| **Reactive** | Waits for user message, responds | Traditional chatbot for FAQs |
| **Scheduled** | Wakes at intervals, checks conditions | Morning briefing at 8am, weekly reports every Friday |
| **Event-Driven** | Listens to webhooks, triggers actions | New lead submitted → qualify → schedule demo |
| **Fully Autonomous** | Sets own goals, prioritizes, acts | Research agent monitors industry news, synthesizes trends, alerts stakeholders |

---

## Multi-Tenant Isolation

Ghost guarantees complete separation between tenants:

| Layer | Isolation Method |
|-------|-----------------|
| **Filesystem** | Each tenant has dedicated directory; no cross-read possible |
| **Network** | Subdomain routing (`acme.ghost.ai`); optional mTLS encryption |
| **Memory** | Separate SQLite database and vector index per tenant |
| **Compute** | Container per tenant (Docker/Podman) or process isolation |

---

## Deployment Scenarios

### Personal/Home (Single Raspberry Pi 5)

```
┌────────────────────────────────────┐
│           Raspberry Pi 5           │
│  ┌──────────────────────────────┐  │
│  │    Ghost Control Plane       │  │
│  │    (Lightweight API)         │  │
│  └──────────────────────────────┘  │
│  ┌──────────┐ ┌──────────┐          │
│  │ Agent 1  │ │ Agent 2  │          │
│  │ (Home)   │ │ (Work)   │          │
│  │ - Lights │ │ - Email  │          │
│  │ - Cal    │ │ - Slack  │          │
│  └──────────┘ └──────────┘          │
│  ┌──────────────────────────────┐  │
│  │ Ollama (Qwen3 4B + Llama 3.2)│  │
│  └──────────────────────────────┘  │
│  ┌──────────────────────────────┐  │
│  │ SQLite (per-agent databases) │  │
│  └──────────────────────────────┘  │
└────────────────────────────────────┘
```

**Constraints:**
- 2-3 agents maximum before RAM pressure
- Agents share Ollama instance (model swapping overhead)
- Skills must be lightweight

---

### Small Business (Pi Cluster)

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Pi 5 #1   │◄───►│   Pi 5 #2   │◄───►│   Pi 5 #3   │
│  (Control)  │     │  (Agents)   │     │  (LLM Pool) │
│  + SQLite   │     │  + Skills   │     │  Ollama x3  │
└─────────────┘     └─────────────┘     └─────────────┘
       │                   │                   │
       └───────────────────┴───────────────────┘
                           │
                    ┌─────────────┐
                    │   Router    │
                    │  (HAProxy)  │
                    └─────────────┘
```

---

### SaaS/Cloud (Multi-Tenant)

Container orchestration enabling thousands of isolated tenant environments with elastic scaling.

---

## Example Scenarios

### Scenario 1: Solo Professional

**User:** Freelance consultant working from home

**Agents:**
- **Executive Agent**: Calendar management, email triage, meeting prep
- **Research Agent**: Daily industry news summary, competitor monitoring
- **Home Agent**: Smart lighting, grocery reminders, package alerts

**Interaction:**
> User: "Start my day"
> 
> Executive Agent: "You have 4 meetings. The 2pm conflicts with your dentist appointment."
> Research Agent: "3 relevant articles on AI regulation published overnight. Summary attached."
> Home Agent: "Coffee ready. Thermostat set to 72°F."

---

### Scenario 2: Small Law Firm

**Tenant:** Acme Legal Associates (acme.ghost.ai)

**Agents:**
- **Intake Agent** (Reactive): Website chat, qualifies potential clients, schedules consultations
- **Case Agent** (Scheduled): Daily deadline checks, filing reminders, document status tracking
- **Billing Agent** (Event-Driven): Generates invoices on case closure, sends payment reminders

**Interaction:**
> New lead submits website form
> 
> Intake Agent: "Thank you for contacting us. Based on your description, this appears to be an employment matter. Are you available Tuesday 2pm or Thursday 10am for a 30-minute consultation?"

---

### Scenario 3: E-commerce Business

**Tenant:** BetaMart (beta.ghost.ai)

**Agents:**
- **Support Agent** (Reactive + Event-Driven): Order status, returns, escalates complex issues
- **Onboarding Agent** (Scheduled): New customer welcome sequences, tutorial delivery
- **Inventory Agent** (Fully Autonomous): Monitors stock levels, predicts reorders, alerts purchasing

**Interaction:**
> Support Agent detects negative sentiment in ticket #4521
> 
> Support Agent: "Escalating to human supervisor. Context: Customer received damaged item twice. Previous resolution: $50 credit. Customer requesting full refund + expedited replacement."

---

### Scenario 4: Healthcare Clinic

**Tenant:** Gamma Health (gamma.ghost.ai)

**Agents:**
- **Scheduling Agent**: Appointment booking, rescheduling, reminder calls
- **Prep Agent** (Scheduled): Pre-visit instructions based on appointment type
- **Follow-up Agent** (Event-Driven): Post-visit care instructions, prescription refill reminders

**Key Feature:** Complete HIPAA isolation—no patient data ever leaves the tenant boundary or trains shared models.

---

## Skill Registry

Agents acquire capabilities through a modular skill system:

| Skill | Function | Example Trigger |
|-------|----------|---------------|
| **Calendar** | Read/write Google/Outlook calendars | "Move my 3pm to Thursday" |
| **Email** | Send, read, draft, categorize | "Email John that I'm running late" |
| **CRM** | Contact management, deal tracking | "Add Acme Corp as a warm lead" |
| **Vision** | Image analysis, OCR, description | "What's the total on this receipt?" |
| **Home** | Smart device control | "Dim living room lights to 30%" |
| **Code** | Script generation, data processing | "Calculate my Q3 expenses from this CSV" |
| **KB** | Knowledge base search, FAQ matching | "What's our return policy?" |
| **Ticket** | Help desk integration, status tracking | "Update ticket #1234 as resolved" |

---

## Roadmap

| Phase | Deliverable | Timeline |
|-------|-------------|----------|
| **Phase 1** | Control Plane + 2-agent local setup | Month 1-2 |
| **Phase 2** | Multi-tenant SaaS infrastructure | Month 2-4 |
| **Phase 4** | Autonomous agent capabilities | Month 4-5 |

---

## The Vision

Ghost OS transforms AI from a single chat interface into a distributed workforce of specialized agents—each with deep expertise, persistent memory, and appropriate autonomy. Whether running on a single Raspberry Pi at home or scaling across thousands of cloud tenants, the architecture maintains core principles:

- **Sovereignty**: Your data, your agents, your control
- **Specialization**: Right agent for the right task
- **Isolation**: Complete separation between tenants and contexts
- **Extensibility**: New skills and agents added without system rebuild

> "Ghost becomes an operating system for AI agents, not just a chatbot."
