# Volcano

This directory contains Volcano configuration, functions, migrations, and local variables.

Local development:

    volcano start
    volcano variables deploy
    volcano functions deploy --all
    volcano migrations deploy --all -d app

If this project includes volcano/volcano-config.yaml:

    volcano config deploy

Cloud deployment:

    volcano login
    volcano use <project-id-or-name>
    volcano cloud variables deploy
    volcano cloud functions deploy --all

If this project includes volcano/volcano-config.yaml:

    volcano cloud config deploy
