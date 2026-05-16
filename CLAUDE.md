# CLAUDE.md

## Project Overview

Eraser is an open-source CLI tool that automatically sends data removal requests to data brokers. This is a **maintained fork** of the original [eraser-privacy/eraser](https://github.com/eraser-privacy/eraser) by Robin Dubreuil. Users provide their personal information, and the tool sends GDPR, CCPA, or generic removal request emails to 760+ data brokers.

## Tech Stack

- **Language**: Go 1.23+
- **CLI Framework**: Cobra (`github.com/spf13/cobra`)
- **Email**: SMTP only
- **Database**: SQLite (for history tracking via `modernc.org/sqlite`)
- **Config**: YAML (`gopkg.in/yaml.v3`)

## Project Structure

```
eraser/
├── cmd/eraser/               # CLI commands (split per command)
│   ├── main.go               # Root command and wiring
│   ├── cmd_send.go
│   ├── cmd_serve.go
│   ├── cmd_init.go
│   ├── cmd_status.go
│   ├── cmd_list_brokers.go
│   ├── cmd_add_broker.go
│   ├── cmd_pipeline.go
│   ├── cmd_monitor.go
│   ├── cmd_confirm.go
│   ├── cmd_fill.go
│   └── cmd_cleanup_bounces.go
├── internal/
│   ├── broker/broker.go     # Broker struct and YAML loading/filtering
│   ├── browser/             # Browser automation for form filling
│   ├── config/config.go     # User configuration (profile, email settings)
│   ├── email/
│   │   ├── sender.go        # Email sender interface
│   │   └── smtp.go          # SMTP implementation
│   ├── history/history.go   # SQLite history tracking
│   ├── inbox/               # Inbox monitoring
│   └── template/
│       ├── template.go      # Template rendering engine
│       └── templates/       # Email templates (embedded)
│           ├── gdpr.tmpl
│           ├── ccpa.tmpl
│           └── generic.tmpl
├── data/brokers.yaml        # 760+ data broker database
└── internal/web/            # Web UI (split handlers)
    ├── server.go            # Server setup and routing
    ├── handlers_pages.go
    ├── handlers_api.go
    ├── handlers_setup.go
    ├── handlers_inbox.go
    ├── handlers_tasks.go
    ├── handlers_settings.go
    ├── job.go
    ├── session.go
    ├── templates/           # HTML templates
    └── static/              # CSS/JS assets
```

## Key Concepts

### Broker
A data broker is a company that collects and sells personal information. Each broker has:
- `id`: Unique lowercase hyphenated identifier (e.g., `spokeo`, `been-verified`)
- `name`: Display name
- `email`: Privacy/removal contact email (required)
- `website`: Company website (optional)
- `opt_out_url`: Direct opt-out link (optional)
- `region`: `us`, `eu`, or `global`
- `category`: `people-search`, `marketing`, or `background-check`

### Templates
Three email templates are available:
- **GDPR**: Invokes EU Article 17 "Right to Erasure"
- **CCPA**: Invokes California Consumer Privacy Act
- **Generic**: General privacy request referencing multiple laws

### Flow
1. Load user config from `~/.eraser/config.yaml`
2. Load brokers from `data/brokers.yaml`
3. Filter by region and exclusions
4. For each broker, render email template with user + broker data
5. Send via SMTP
6. Record result in SQLite history

## Common Commands

```bash
# Build the project
go build -o eraser ./cmd/eraser

# Run tests
go test ./...

# List all brokers
./eraser list-brokers

# Preview emails without sending
./eraser send --dry-run

# Send removal requests
./eraser send

# View send history
./eraser status
```

## Configuration

User config is stored at `~/.eraser/config.yaml`. See `config.example.yaml` for the full schema. Key sections:
- `profile`: User's personal info (name, address, etc.)
- `email`: Provider settings (SMTP)
- `options`: Template choice, rate limiting, region filters

## Adding Brokers

Brokers are defined in `data/brokers.yaml`. Required fields:
```yaml
- id: example-broker
  name: Example Broker
  email: privacy@example.com
  region: us
  category: marketing
```

The broker database now includes 760+ brokers from the Privacy Rights Clearinghouse registry.

## Code Patterns

- **Error handling**: Wrap errors with context using `fmt.Errorf("context: %w", err)`
- **Config loading**: Uses YAML with struct tags for marshaling
- **Templates**: Go `text/template` with embedded files via `//go:embed`
- **CLI commands**: Defined in `cmd/eraser/main.go` using Cobra

## Important Files

| File | Purpose |
|------|---------|
| `cmd/eraser/main.go` | Root command and wiring |
| `cmd/eraser/cmd_send.go` | Send command logic |
| `cmd/eraser/cmd_serve.go` | Web UI server command |
| `cmd/eraser/cmd_init.go` | Interactive config setup |
| `internal/broker/broker.go` | Broker type and database operations |
| `internal/email/sender.go` | Email sender interface |
| `internal/email/smtp.go` | SMTP implementation |
| `internal/template/template.go` | Email template rendering |
| `internal/web/server.go` | Web server setup and routing |
| `internal/web/handlers_pages.go` | Page handlers |
| `internal/web/handlers_api.go` | API handlers |
| `internal/web/handlers_setup.go` | Setup wizard handlers |
| `data/brokers.yaml` | Data broker database (760+ entries) |
| `config.example.yaml` | Example user configuration |

## Security Notes

- Never commit user configs (contains personal data)
- Config file should have 0600 permissions
- Use app passwords, not main email passwords
- Email credentials should use environment variables in CI

## Current Development Status

**This is a maintained fork** of [eraser-privacy/eraser](https://github.com/eraser-privacy/eraser).

**Completed Phases:**

1. **Phase 1: Foundation** - Web server with Chi router, Tailwind/HTMX, dashboard
2. **Phase 2: Setup Wizard** - Multi-step wizard for profile and email config
3. **Phase 3: Broker Management UI** - Search/filter, status display, individual/bulk send
4. **Phase 4: History UI** - History list with partial template for HTMX updates
5. **Phase 5: Pipeline & Inbox** - Inbox monitoring, form filling, bounce cleanup
6. **Phase 6: Polish** - CSRF protection with gorilla/csrf, security headers

## Web UI Architecture

**Key Files:**
- `internal/web/server.go` - Server setup, routing, CSRF protection
- `internal/web/handlers_pages.go` - Dashboard and page rendering
- `internal/web/handlers_api.go` - JSON API endpoints
- `internal/web/handlers_setup.go` - Setup wizard (state in cookies)
- `internal/web/handlers_inbox.go` - Inbox monitoring handlers
- `internal/web/handlers_tasks.go` - Background task handlers
- `internal/web/handlers_settings.go` - Settings management
- `internal/web/templates/` - HTML templates (layout.html, dashboard.html, brokers.html, history.html, setup/*.html)
- `internal/web/templates/partials/` - HTMX partial templates (broker-list.html, history-list.html)
- `internal/web/static/` - Tailwind CSS and HTMX JS

**Email Provider Supported:**
- `smtp` - Traditional SMTP (Gmail, Outlook, any SMTP server)

**Running the Web UI:**
```bash
./eraser serve          # Starts on localhost:8080
./eraser serve -p 3000  # Custom port
```

**Key Features:**
- Setup wizard for first-time configuration
- Broker list with search, filter by category/region
- Individual and bulk send with real-time progress
- History tracking and status display
- CSRF protection for all forms and AJAX requests

**Config Structure** (`internal/config/config.go`):
```go
type EmailConfig struct {
    Provider string     // "smtp"
    From     string
    SMTP     SMTPConfig
}
```
