# Mail delivery

Transactional email leaves the application through the `mail.Sender` port in
`backend/internal/platform/mail`. Email verification and password reset use
this port in JOOXF-297 and JOOXF-298; this package contains no verification or
reset rules.

## Port

`Sender.Send(ctx, mail.Message)` delivers one plain-text email and returns an
error without retrying when delivery fails. A `Message` carries `To` (bare
addresses), `Subject`, and `Text`. Validation rejects missing recipients,
malformed addresses, blank subjects, and header line breaks.

The sender address belongs to the adapter configuration, so every message from
one process uses the same verified sender.

## SMTP adapter

`backend/internal/platform/mail/adapters/smtp` implements the port over SMTP.
It uses STARTTLS when the server advertises it and supports optional plain
authentication for a real provider later.

| Variable | Default | Meaning |
|---|---|---|
| `MAIL_SMTP_ADDR` | `127.0.0.1:11025` | SMTP endpoint as `host:port`. |
| `MAIL_FROM` | `no-reply@xflickr.local` | Envelope and header sender address. |
| `MAIL_SMTP_USERNAME` | empty | Optional SMTP username. |
| `MAIL_SMTP_PASSWORD` | empty | Optional SMTP password. |

Leave both credentials empty for the development sink. The settings are loaded
by `platform/config` and listed in `.env.example`. They are infrastructure
endpoints, so they stay deployment configuration rather than admin settings.

The rules and normalisation for the address, sender, and credential pair live
in one place, `mail.SMTPSettings.Validate`. Both `platform/config` and the SMTP
adapter call it, so a rule changes in a single implementation.

## Development sink

Development and tests send to the shared Mailpit instance on the Dev Workspaces
VM:

- SMTP: `127.0.0.1:11025`
- HTTP API and UI: `127.0.0.1:18025`

Mailpit accepts any recipient and sender without authentication and never
forwards mail. The integration test sends one message through the adapter and
reads it back from `GET /api/v1/messages` and `/api/v1/message/{id}`, so it needs
the shared sink and runs only with `XFLICKR_INTEGRATION_TEST=1`.

## Fake sender

`mail.NewFake()` returns an in-memory `Sender` for unit tests. It records
validated messages in send order, returns copies from `Sent()`, and can fail
subsequent sends through `SetError` without recording them.
