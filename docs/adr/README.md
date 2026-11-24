# Architecture decision records

Short records of decisions that shape gocpp and are costly to reverse. Each says
what was decided, why, and what it costs. They describe the code as it is; when a
decision changes, a new record supersedes the old one instead of editing it.

| # | Decision | Status |
|---|---|---|
| [0001](0001-one-reader-goroutine-per-connection.md) | One resident goroutine per connection | Accepted |
| [0002](0002-message-id-correlation.md) | Responses are matched to calls by message id | Accepted |
| [0003](0003-schemas-to-codegen.md) | Message types, registrations and schemas are generated from the OCA JSON schemas | Accepted |
| [0004](0004-schema-validation-modes.md) | Schema validation is a four-way mode; off by default, lenient for production | Accepted |
| [0005](0005-compat-defaults-follow-upstream.md) | The ocpp-go drop-in behaves like upstream by default | Accepted |
| [0006](0006-gateway-split.md) | Optional split into a connection tier and a processing tier | Accepted (experimental) |

To add one: copy [0000-template.md](0000-template.md) to the next number, keep it
to a page, state facts that can be checked in the code or in the docs it links, and
add a row above.
