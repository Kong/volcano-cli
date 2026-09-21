# CLI acceptance tests

Set `VOLCANO_TEST_CLI_BINARY` to an installed CLI to test the customer artifact.
Both API and local-mode suites validate the executable and use it without building
CLI source. Without an override, the suites build the current checkout.

API commands explicitly receive `VOLCANO_API_URL`; release binaries do not need
custom linker settings. Local-mode tests continue to use `VOLCANO_IMAGE` for the
server under test. The existing Make targets and fixture environment apply.

CI candidate downloads lose executable permissions. Verify the selected binary's
attestation against `acceptance.yml` and its exact source ref and commit, then run
`chmod +x <binary>` before setting `VOLCANO_TEST_CLI_BINARY`. Hosting's installer
performs both steps. Candidate builds use the same authentication and default
endpoint settings as the release workflow.
