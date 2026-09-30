# Commands and configuration

## Commands

| Command | What it does |
|---|---|
| `ghost` | Chat in the terminal |
| `ghost pair [name]` | Show a QR to connect the app |
| `ghost tasks` | Routines: list, pause, resume, cancel |
| `ghost ideas` | Suggestions with the evidence behind them |
| `ghost model [list\|use provider:model]` | See or change the model |
| `ghost mcp list\|add\|edit\|remove\|test` | Tool servers |
| `ghost connector ...` | Portable connectors (validate, install, sign) |
| `ghost skills ...` | List, add, remove, enable, disable skills |
| `ghost state backup\|export\|import\|inspect\|prune` | Snapshots and portable state |
| `ghost update` | Update Ghost |
| `ghost auth reset-password` | Reset the console password |
| `ghost status`, `ghost verify` | Health and the real product checks |
| `ghost reset` | Factory reset (with exclusions) |
| `ghost relay ...` | Your own relay |

## Inside a chat

`/memory`, `/activity`, `/devices`, `/files`, `/attach <path>`, `/model`,
`/context`, `/routines`, `/tasks`, `/ideas`, `/thread`, `/main`, `/rewind`,
`/details`, `/clear`, `/help`.

## Configuration

Precedence, highest first: environment variables, then `.secrets.json` (sealed;
API keys and tokens), then `config.json` (model, providers, channels), then
defaults. Secrets are set from the console or app, never by editing files. `.env`
holds only system settings (`GHOST_API_PORT`, `TZ`).

| Where | What |
|---|---|
| `/var/ghost/config/` | `config.json`, sealed secrets, the master key. Mode `0700`. |
| `/var/lib/ghost/workspace/` | Identity, memory, skills, sessions, files. Owned by whoever installed Ghost. |
| `/var/lib/ghost/backups/` | Recovery snapshots. |

The services run as root; every file they create in your workspace and config is
handed back to the account that installed Ghost, so your own terminal can always
read them.
