# Credit gating

The Volcano API can tell the CLI about a project's billing/credit status
using the same instruction protocol it uses for
[version gating](#relationship-to-version-gating). Two instruction values are
reserved for this:

| Instruction         | Meaning                                   | Blocking? |
| ------------------- | ----------------------------------------- | --------- |
| `low_credit_warning`| The project is running low on credit.     | No        |
| `not_enough_credit` | The project is out of credit for a request. | Server-enforced |

The CLI receives these as an `X-Volcano-CLI-Instruction` response header on a
command it already ran — there is no extra network round-trip.

> **Dormant today.** The server does not emit either header yet (the
> billing-service integration is tracked separately). The CLI-side handling
> described here is fully wired and tested, so nothing on the CLI needs to
> change when the server starts sending the header.

## What the CLI does

When a credit instruction is present, the CLI prints a concise, neutral notice
to stderr followed by an actionable billing link:

```text
Your project is running low on credit.
Purchase credits at: https://volcano.dev/billing?source=cli
```

```text
Your project does not have enough credit to complete this request.
Purchase credits at: https://volcano.dev/billing?source=cli
```

The notice never claims the command was blocked. Because the instruction is
observed *after* the command has run, the CLI cannot retroactively cancel a
request that already completed — it only surfaces the situation and the link.
Actual enforcement of `not_enough_credit` is a server responsibility (for
example, failing the request with an appropriate status); when that happens the
CLI reports the server's error like any other failure. A credit notice on its
own never changes the command's exit code.

### The billing link

The "purchase credits" URL is derived from the CLI's configured web origin
(`https://volcano.dev` by default), with `VOLCANO_WEB_URL` taking precedence:

```bash
VOLCANO_WEB_URL=https://staging.example.com volcano ...
# -> https://staging.example.com/billing?source=cli
```

### Interactive prompt

For interactive, prompt-safe commands the CLI additionally offers to open the
billing page in your browser:

```text
Open the billing page to purchase credits? [y/N]:
```

- Answering `y`/`yes` opens the billing page in your default browser. If the
  browser cannot be opened, the CLI prints the URL so you can open it manually.
- Any other answer (including a bare Enter) declines; the CLI just prints the
  URL.

The prompt is **only** shown when all of the following hold, so scripts and
automation are never affected:

1. The command is explicitly marked prompt-safe (finite, interactive commands
   opt in; long-running/streaming commands such as `logs --follow` never do).
2. No CI environment is detected (`CI` is unset).
3. Both stdin and stderr are terminals.

In any non-interactive context — pipes, CI, redirected output, or an
unmarked command — the prompt is skipped and only the notice and billing URL
are printed. Declining, a prompt error, or a browser-open failure never change
the command's exit status.

## Relationship to version gating

Credit gating shares the `X-Volcano-CLI-Instruction` transport with the CLI
version-gating protocol (`suggestion_version_upgrade`,
`require_version_upgrade`). Version deprecation can hard-block a request with
HTTP 426; credit notices, by contrast, are surfaced after the fact and do not
themselves block.
