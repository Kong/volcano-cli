# Changelog

## [0.23.1](https://github.com/Kong/volcano-cli/compare/v0.23.0...v0.23.1) (2026-09-01)


### Bug Fixes

* **config:** accept function invocation metadata ([#177](https://github.com/Kong/volcano-cli/issues/177)) ([81ebaf7](https://github.com/Kong/volcano-cli/commit/81ebaf76a938a9321649099fb786472a5ac9dfb3))
* **release:** bypass reviews after checks ([#179](https://github.com/Kong/volcano-cli/issues/179)) ([8f58add](https://github.com/Kong/volcano-cli/commit/8f58addb4f4bfe91d2a7c8e2954195e9c64dceaa))

## [0.23.0](https://github.com/Kong/volcano-cli/compare/v0.22.1...v0.23.0) (2026-08-26)


### Features

* **databases:** manage database backups and restores from the CLI ([#168](https://github.com/Kong/volcano-cli/issues/168)) ([b4209aa](https://github.com/Kong/volcano-cli/commit/b4209aab8cd3ca1902e8c2b9c2d3a718c920e095))


### Bug Fixes

* **databases:** show branch names the API accepts and say when a string is withheld ([#169](https://github.com/Kong/volcano-cli/issues/169)) ([205f3e2](https://github.com/Kong/volcano-cli/commit/205f3e24221719cd97496a2ebaef6ee7948f0ffd))
* **localmode:** regenerate the compose template from volcano-hosting ([#172](https://github.com/Kong/volcano-cli/issues/172)) ([7b68d7e](https://github.com/Kong/volcano-cli/commit/7b68d7e8527384b97aa705fa34fca0d7ba6ea029))

## [0.22.1](https://github.com/Kong/volcano-cli/compare/v0.22.0...v0.22.1) (2026-08-20)


### Bug Fixes

* **git:** follow git's push routing, and correct a wrong justification from [#159](https://github.com/Kong/volcano-cli/issues/159) ([#165](https://github.com/Kong/volcano-cli/issues/165)) ([a8e6bf1](https://github.com/Kong/volcano-cli/commit/a8e6bf1e0b961313dd569c2de2de7695efa189a3))

## [0.22.0](https://github.com/Kong/volcano-cli/compare/v0.21.2...v0.22.0) (2026-08-20)


### Features

* **git:** connect and disconnect a project's GitHub repository ([#159](https://github.com/Kong/volcano-cli/issues/159)) ([e455992](https://github.com/Kong/volcano-cli/commit/e4559925ce526bec2dccfadd3e6b415138cdef20))

## [0.21.2](https://github.com/Kong/volcano-cli/compare/v0.21.1...v0.21.2) (2026-08-20)


### Bug Fixes

* **databases:** accept the 202 a branch reset now returns ([#162](https://github.com/Kong/volcano-cli/issues/162)) ([1a8bceb](https://github.com/Kong/volcano-cli/commit/1a8bceb05f7483cae0dfe34e0efc3a45ca92bb0b))

## [0.21.1](https://github.com/Kong/volcano-cli/compare/v0.21.0...v0.21.1) (2026-08-19)


### Bug Fixes

* **databases:** use an underscore branch name in the cloud E2E test ([#160](https://github.com/Kong/volcano-cli/issues/160)) ([823feb1](https://github.com/Kong/volcano-cli/commit/823feb193b1e618356155b7228c98dccefb4f386))

## [0.21.0](https://github.com/Kong/volcano-cli/compare/v0.20.0...v0.21.0) (2026-08-18)


### Features

* **databases:** manage database branches from the CLI ([#157](https://github.com/Kong/volcano-cli/issues/157)) ([5a6e7c8](https://github.com/Kong/volcano-cli/commit/5a6e7c858ce018ceb047e88c58d50b6edb01b2ca))

## [0.20.0](https://github.com/Kong/volcano-cli/compare/v0.19.4...v0.20.0) (2026-08-09)


### Features

* email domain lock ([#153](https://github.com/Kong/volcano-cli/issues/153)) ([f1ae924](https://github.com/Kong/volcano-cli/commit/f1ae92473a6c83fad44891a70dca33994a07b2fc))

## [0.19.4](https://github.com/Kong/volcano-cli/compare/v0.19.3...v0.19.4) (2026-08-03)


### Bug Fixes

* **localmode:** sync compose template with volcano-hosting ([#151](https://github.com/Kong/volcano-cli/issues/151)) ([b2626fd](https://github.com/Kong/volcano-cli/commit/b2626fddbd3e5ba0af379d794029c0be94e3e685))

## [0.19.3](https://github.com/Kong/volcano-cli/compare/v0.19.2...v0.19.3) (2026-07-30)


### Bug Fixes

* **logs:** match server LogEvent body field, docs fixes (VOL-648 sub-tickets) ([#149](https://github.com/Kong/volcano-cli/issues/149)) ([057dbac](https://github.com/Kong/volcano-cli/commit/057dbacc011611245a1c956b1eb47a18ee2b6a40))

## [0.19.2](https://github.com/Kong/volcano-cli/compare/v0.19.1...v0.19.2) (2026-07-30)


### Bug Fixes

* **ci:** retry gh release API calls in publish-cli on transient 5xx ([#145](https://github.com/Kong/volcano-cli/issues/145)) ([8ee30c5](https://github.com/Kong/volcano-cli/commit/8ee30c5bfb2dc90138461a78992079009d19b026))

## [0.19.1](https://github.com/Kong/volcano-cli/compare/v0.19.0...v0.19.1) (2026-07-30)


### Bug Fixes

* **setup:** allow --yes alongside --agent and --manual ([#146](https://github.com/Kong/volcano-cli/issues/146)) ([133d2d7](https://github.com/Kong/volcano-cli/commit/133d2d7ff459d0cab5172824071d97600f117b73))

## [0.19.0](https://github.com/Kong/volcano-cli/compare/v0.18.0...v0.19.0) (2026-07-30)


### ⚠ BREAKING CHANGES

* **ci:** remove nightly release channel ([#142](https://github.com/Kong/volcano-cli/issues/142))

### Miscellaneous Chores

* **ci:** remove nightly release channel ([#142](https://github.com/Kong/volcano-cli/issues/142)) ([e0bb106](https://github.com/Kong/volcano-cli/commit/e0bb10619f8d1a01311f1e3c64f6b07770974c70))

## [0.18.0](https://github.com/Kong/volcano-cli/compare/v0.17.0...v0.18.0) (2026-07-29)


### Features

* **docs:** source docs from volcano-docs and weight CLI section higher ([#134](https://github.com/Kong/volcano-cli/issues/134)) ([936aabd](https://github.com/Kong/volcano-cli/commit/936aabd87d35f665e7e7d3abdddfb3ecc940cded))

## [0.17.0](https://github.com/Kong/volcano-cli/compare/v0.16.0...v0.17.0) (2026-07-29)


### ⚠ BREAKING CHANGES

* **setup:** rename --harness flag to --agent ([#139](https://github.com/Kong/volcano-cli/issues/139))

### Features

* **setup:** rename --harness flag to --agent ([#139](https://github.com/Kong/volcano-cli/issues/139)) ([8a29003](https://github.com/Kong/volcano-cli/commit/8a2900314e9d6d7f5756ed8f16098313fec77cf0))

## [0.16.0](https://github.com/Kong/volcano-cli/compare/v0.15.2...v0.16.0) (2026-07-29)


### Features

* **ci:** auto-bump Homebrew tap on stable release ([#135](https://github.com/Kong/volcano-cli/issues/135)) ([492a8e4](https://github.com/Kong/volcano-cli/commit/492a8e4b07be97841a7ce3c0d9db11d46990ad53))

## [0.15.2](https://github.com/Kong/volcano-cli/compare/v0.15.1...v0.15.2) (2026-07-28)


### Bug Fixes

* **api:** clearer error when the Volcano API is unreachable ([#130](https://github.com/Kong/volcano-cli/issues/130)) ([91a6282](https://github.com/Kong/volcano-cli/commit/91a6282f830b9fd994df643475cd1f3d98d6be8d))

## [0.15.1](https://github.com/Kong/volcano-cli/compare/v0.15.0...v0.15.1) (2026-07-28)


### Bug Fixes

* **setup:** color CTA heading volcano-400 ([#129](https://github.com/Kong/volcano-cli/issues/129)) ([46eb79a](https://github.com/Kong/volcano-cli/commit/46eb79abe3d7a893a9ca1217bca94e4ef959be88))

## [0.15.0](https://github.com/Kong/volcano-cli/compare/v0.14.1...v0.15.0) (2026-07-27)


### Features

* **setup:** give space/enter/esc key hints distinct colors ([#126](https://github.com/Kong/volcano-cli/issues/126)) ([7110614](https://github.com/Kong/volcano-cli/commit/71106143ccac71a6fda68c8d0eed446193d5acdc))

## [0.14.1](https://github.com/Kong/volcano-cli/compare/v0.14.0...v0.14.1) (2026-07-27)


### Bug Fixes

* better deployments ([#79](https://github.com/Kong/volcano-cli/issues/79)) ([e665f4b](https://github.com/Kong/volcano-cli/commit/e665f4b5cd79ca06036952042eb2d66cba2dbf0d))

## [0.14.0](https://github.com/Kong/volcano-cli/compare/v0.13.1...v0.14.0) (2026-07-27)


### Features

* **setup:** render setup CTA example prompt in volcano-600 ([#121](https://github.com/Kong/volcano-cli/issues/121)) ([6b2eeee](https://github.com/Kong/volcano-cli/commit/6b2eeee21e8c93b619b2f85b4a66e002c937ba03))

## [0.13.1](https://github.com/Kong/volcano-cli/compare/v0.13.0...v0.13.1) (2026-07-27)


### Bug Fixes

* **ci:** nightly publish hits GitHub 1000-asset-per-release cap ([#120](https://github.com/Kong/volcano-cli/issues/120)) ([fc4aa62](https://github.com/Kong/volcano-cli/commit/fc4aa6273197c76c9a93782422f3b7aa26f42184))

## [0.13.0](https://github.com/Kong/volcano-cli/compare/v0.12.1...v0.13.0) (2026-07-27)


### Features

* **cli:** apply setup color theme across all command output ([#115](https://github.com/Kong/volcano-cli/issues/115)) ([f909d16](https://github.com/Kong/volcano-cli/commit/f909d16ed7d043dce2060c07cf1cfcad08d9d46a))

## [0.12.1](https://github.com/Kong/volcano-cli/compare/v0.12.0...v0.12.1) (2026-07-27)


### Bug Fixes

* **localmode:** correct prod image default and first-party env forwarding for volcano start ([#116](https://github.com/Kong/volcano-cli/issues/116)) ([8eaf3c5](https://github.com/Kong/volcano-cli/commit/8eaf3c564c28eeb53317c7df032848420aee55fe))

## [0.12.0](https://github.com/Kong/volcano-cli/compare/v0.11.0...v0.12.0) (2026-07-27)


### Features

* **setup:** update marketplace plugins on rerun instead of no-op ([#113](https://github.com/Kong/volcano-cli/issues/113)) ([b6ce3d3](https://github.com/Kong/volcano-cli/commit/b6ce3d30e7f2aad8613b662d2ba18eef2bcd3b6b))

## [0.11.0](https://github.com/Kong/volcano-cli/compare/v0.10.0...v0.11.0) (2026-07-26)


### Features

* **setup:** replace install spinner with looping volcano eruption animation ([#111](https://github.com/Kong/volcano-cli/issues/111)) ([24d45f9](https://github.com/Kong/volcano-cli/commit/24d45f93b488b703df5362b719b9f4336b6fae8a))

## [0.10.0](https://github.com/Kong/volcano-cli/compare/v0.9.0...v0.10.0) (2026-07-26)


### Features

* **setup:** full-word status marks in the report ([#108](https://github.com/Kong/volcano-cli/issues/108)) ([5fc0077](https://github.com/Kong/volcano-cli/commit/5fc0077a82500061d449541755986868b08caa9b))

## [0.9.0](https://github.com/Kong/volcano-cli/compare/v0.8.0...v0.9.0) (2026-07-26)


### Features

* **setup:** interactive, agent-safe harness selection ([#107](https://github.com/Kong/volcano-cli/issues/107)) ([458767c](https://github.com/Kong/volcano-cli/commit/458767c039c858045fd8e3a708c665565a5a7850))

## [0.8.0](https://github.com/Kong/volcano-cli/compare/v0.7.2...v0.8.0) (2026-07-26)


### Features

* **doctor:** add volcano doctor local-dev prerequisite checks ([#105](https://github.com/Kong/volcano-cli/issues/105)) ([fbd15a4](https://github.com/Kong/volcano-cli/commit/fbd15a4aad180e7f7b352b2eb82c2212572b2719))

## [0.7.2](https://github.com/Kong/volcano-cli/compare/v0.7.1...v0.7.2) (2026-07-26)


### Bug Fixes

* **setup:** keep the real reason on detected-but-failed harnesses ([#102](https://github.com/Kong/volcano-cli/issues/102)) ([0e3944d](https://github.com/Kong/volcano-cli/commit/0e3944de9619a78cd24235f14bcae2b9f10643c6))

## [0.7.1](https://github.com/Kong/volcano-cli/compare/v0.7.0...v0.7.1) (2026-07-26)


### Bug Fixes

* **upgrade:** skip package-manager reinstall when already on the latest release ([#99](https://github.com/Kong/volcano-cli/issues/99)) ([33ede5f](https://github.com/Kong/volcano-cli/commit/33ede5f98b1d7b259c76830f509257621674645e))

## [0.7.0](https://github.com/Kong/volcano-cli/compare/v0.6.3...v0.7.0) (2026-07-26)


### Features

* **setup:** print a build CTA after a successful install ([#98](https://github.com/Kong/volcano-cli/issues/98)) ([264f2f6](https://github.com/Kong/volcano-cli/commit/264f2f69a99fcc1d5f80ba0b20c128751423b383))

## [0.6.3](https://github.com/Kong/volcano-cli/compare/v0.6.2...v0.6.3) (2026-07-25)


### Bug Fixes

* **setup:** tolerate already-registered plugins on marketplace rerun ([#96](https://github.com/Kong/volcano-cli/issues/96)) ([7866a9e](https://github.com/Kong/volcano-cli/commit/7866a9e730085d3257311f2ba4026a4a45fd6804))

## [0.6.2](https://github.com/Kong/volcano-cli/compare/v0.6.1...v0.6.2) (2026-07-25)


### Bug Fixes

* **setup:** show only harnesses that install, drop the rest ([#94](https://github.com/Kong/volcano-cli/issues/94)) ([00da55f](https://github.com/Kong/volcano-cli/commit/00da55fb54d79eec4092413d5f8d2685f241fb49))

## [0.6.1](https://github.com/Kong/volcano-cli/compare/v0.6.0...v0.6.1) (2026-07-25)


### Bug Fixes

* **ci:** default all CLI release builds to staging so latest/npm install staging during the testing phase ([#92](https://github.com/Kong/volcano-cli/issues/92)) ([a2c3140](https://github.com/Kong/volcano-cli/commit/a2c3140c7bc2c8dbbf94c5e168d4eafed05a3f27))

## [0.6.0](https://github.com/Kong/volcano-cli/compare/v0.5.0...v0.6.0) (2026-07-25)


### Features

* **setup:** add volcano setup to install agent skills/plugins into detected harnesses ([#88](https://github.com/Kong/volcano-cli/issues/88)) ([7a46b68](https://github.com/Kong/volcano-cli/commit/7a46b6880bad967e9aee09d31bddc253d554a162))


### Bug Fixes

* default CLI release builds to staging for testing phase ([#68](https://github.com/Kong/volcano-cli/issues/68)) ([99daffe](https://github.com/Kong/volcano-cli/commit/99daffee2b109d0449b88ecd00a74151ccd0cb72))

## [0.5.0](https://github.com/Kong/volcano-cli/compare/v0.4.0...v0.5.0) (2026-07-23)


### Features

* **cli:** add 'volcano projects keys' command to retrieve a project's anon key ([#86](https://github.com/Kong/volcano-cli/issues/86)) ([4b05290](https://github.com/Kong/volcano-cli/commit/4b0529070a8abc870c86e1296498d36efb938258))

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
