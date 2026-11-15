# Migrations

Place SQL migrations in this directory. Files run in name order, each once:
Volcano records the files it applies in the database's migration history and
skips them on later runs. To change the schema again, add a new file rather
than editing one that ran. Run them locally with:

    volcano deploy --with-migrations

or on their own:

    volcano databases migration apply -d app
