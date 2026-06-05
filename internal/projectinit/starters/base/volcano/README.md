# Volcano

This directory contains Volcano configuration, functions, migrations, and local variables.

Local development:

    volcano start
    volcano local variables deploy
    volcano local functions deploy --all
    volcano local migrations deploy --all -d app

If this project includes volcano/volcano-config.yaml:

    volcano local config deploy

Cloud deployment:

    volcano login
    volcano use <project-id-or-name>
    volcano variables deploy
    volcano functions deploy --all

If this project includes volcano/volcano-config.yaml:

    volcano config deploy
