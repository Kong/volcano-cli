# Migrations

Place SQL migrations in this directory, one statement per file. Files run in
name order, every time, so write them to be safe to repeat. Run them locally
with:

    volcano deploy --with-migrations

or on their own:

    volcano migrations deploy --all -d app
