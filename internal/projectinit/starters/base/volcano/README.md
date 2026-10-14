# Volcano

This directory contains Volcano migrations and local variables.

Local development:

    volcano start
    volcano deploy --with-migrations

`volcano deploy` deploys whatever the project has: the variables in
volcano/volcano.env, the migrations in volcano/migrations, the functions in
volcano/functions, the frontends listed in .volcano/source-layout.json, and
volcano/volcano-config.yaml. Migrations run again on every --with-migrations
run, so write them to be safe to repeat.

Cloud deployment:

    volcano login                          # new to Volcano? run: volcano signup
    volcano use <project-id-or-name>
    volcano cloud deploy --env-file volcano/cloud.env

volcano/volcano.env holds the local server's URL and keys, which a cloud
project can't use; `volcano cloud deploy` refuses it. Put the cloud project's
variables in volcano/cloud.env, under the same names. Add
--with-migrations -d <database> to apply the migrations to a cloud database.
Run `volcano deploy --help` for every option and the commands that deploy one
kind of resource at a time.
