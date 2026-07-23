# Changelog

## [0.4.0](https://github.com/Kong/volcano-cli/compare/v0.3.0...v0.4.0) (2026-07-23)


### Features

* **cli:** add --debug / VOLCANO_DEBUG to trace API requests and responses ([#84](https://github.com/Kong/volcano-cli/issues/84)) ([03d043e](https://github.com/Kong/volcano-cli/commit/03d043ed8f3acfefb272b87c7d98b16335f663ba))

## [0.3.0](https://github.com/Kong/volcano-cli/compare/v0.2.2...v0.3.0) (2026-07-23)


### Features

* **upgrade:** upgrade via the package manager the CLI was installed with ([#81](https://github.com/Kong/volcano-cli/issues/81)) ([1e37498](https://github.com/Kong/volcano-cli/commit/1e37498aba5e2309b116565b9065c4f417f40bf7))


### Bug Fixes

* **local:** pull the latest default local-mode image on volcano start ([#82](https://github.com/Kong/volcano-cli/issues/82)) ([c44df04](https://github.com/Kong/volcano-cli/commit/c44df04155bfe583692b9769c2121970173eceb3))

## [0.2.2](https://github.com/Kong/volcano-cli/compare/v0.2.1...v0.2.2) (2026-07-23)


### Bug Fixes

* **local:** send no credential in local mode so all commands work the same way ([#78](https://github.com/Kong/volcano-cli/issues/78)) ([eb97e07](https://github.com/Kong/volcano-cli/commit/eb97e07b8c3ee4af7d48a299a0a03e7257141a90))

## [0.2.1](https://github.com/Kong/volcano-cli/compare/v0.2.0...v0.2.1) (2026-07-22)


### Bug Fixes

* **auth:** stop routing login through a Volcano Web page that doesn't exist for it ([#75](https://github.com/Kong/volcano-cli/issues/75)) ([294d66c](https://github.com/Kong/volcano-cli/commit/294d66c4a4d27a846d3b4bd7fb1a88275b7aa2d9))

## [0.2.0](https://github.com/Kong/volcano-cli/compare/v0.1.3...v0.2.0) (2026-07-22)


### Features

* **config:** derive VOLCANO_WEB_URL from VOLCANO_API_URL by convention ([#74](https://github.com/Kong/volcano-cli/issues/74)) ([3f2d7bf](https://github.com/Kong/volcano-cli/commit/3f2d7bfd3081effdfaef6661558d14395ca1f897))

## [0.1.3](https://github.com/Kong/volcano-cli/compare/v0.1.2...v0.1.3) (2026-07-22)


### Bug Fixes

* **auth:** route browser login through Volcano Web's login page ([#72](https://github.com/Kong/volcano-cli/issues/72)) ([cb74671](https://github.com/Kong/volcano-cli/commit/cb746711fd827f8d33edeb9b840ca5c9452c08de))

## [0.1.2](https://github.com/Kong/volcano-cli/compare/v0.1.1...v0.1.2) (2026-07-22)


### Bug Fixes

* **api:** suppress non-JSON error bodies that look like markup or are too large ([#69](https://github.com/Kong/volcano-cli/issues/69)) ([41d7cce](https://github.com/Kong/volcano-cli/commit/41d7cce0a9c93301be458d2f4310109e7377dca8))

## [0.1.1](https://github.com/Kong/volcano-cli/compare/v0.1.0...v0.1.1) (2026-07-15)


### Continuous Integration

* add Release Please stable release flow with GitHub App token [VOL-367] ([#38](https://github.com/Kong/volcano-cli/issues/38)) ([4729595](https://github.com/Kong/volcano-cli/commit/4729595bf3002fbe7218fd23985d2613d1b31576))
