# CLI acceptance tests

Set `VOLCANO_TEST_CLI_BINARY` to an installed CLI to test the customer artifact.
Both API and local-mode suites validate the executable and use it without building
CLI source. Without an override, the suites build the current checkout.

API commands explicitly receive `VOLCANO_API_URL`; release binaries do not need
custom linker settings. Local-mode tests continue to use `VOLCANO_IMAGE` for the
server under test. The existing Make targets and fixture environment apply.
