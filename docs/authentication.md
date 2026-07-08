# Authentication

New to Volcano? Create an account from the CLI:

```bash
volcano signup
```

`volcano signup` prefills your email from `git config --global user.email`
when available (press Enter to accept, or type a different address), then
opens Volcano's web signup flow in your browser. Once you finish in the
browser, the CLI completes the device-authorization handshake and saves your
credentials to `~/.volcano/config.json`, so a single command signs you up
and logs you in.

Already have an account? Authenticate with `volcano login`:

```bash
# Browser-based login (default)
volcano login

# Token-based login (for CI/CD)
volcano login --token pk-xxxxxxxxxx

# Or skip login entirely with an environment variable
export VOLCANO_TOKEN=pk-xxxxxxxxxx
```

Log out at any time:

```bash
volcano logout
```

This deletes local credentials but does not revoke the token. Revoke it in the
Volcano dashboard to fully cut off access.

To target a non-production environment, set `VOLCANO_API_URL` to that backend's
API endpoint. `volcano signup` then opens the signup page on the same
environment the API points at. It follows the device-flow verification URL,
just like `volcano login`, so you normally do not need anything else. Set
`VOLCANO_WEB_URL` only to force a specific web origin.
