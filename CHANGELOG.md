# Changelog

All notable changes to setup-browser-tools will be documented in this file. Each release's section is added
by its release PR (see [RELEASE.md](RELEASE.md)) and becomes its GitHub release notes.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.0] - 2026-10-29

### What's Changed
* Start a virtual display so headed browser tests run on Linux by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/43
* Cache Firefox where a later pipeline can restore it by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/39
* Check a driver in its install directory and put that on PATH by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/40
* Install Firefox and Chrome for Testing on Linux ARM64 by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/41
* Export CHROME_BIN and FIREFOX_BIN so later steps can find the browser by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/42
* Keep macOS firefox and chrome runnable after the install step by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/44
* Pin Firefox's signature check to Mozilla's release key by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/45
* Update README.md by @kevin-adoboe in https://github.com/circleci-functions/setup-browser-tools/pull/47
* Keep an installed Firefox when the latest is requested by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/48
* Speed up install-firefox on Debian and Ubuntu by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/49
* Update all non-major dependencies by @renovate[bot] in https://github.com/circleci-functions/setup-browser-tools/pull/35

### New Contributors
* @kevin-adoboe made their first contribution in https://github.com/circleci-functions/setup-browser-tools/pull/47

**Full Changelog**: https://github.com/circleci-functions/setup-browser-tools/compare/v0.4.0...v0.5.0

## [0.4.0] - 2026-10-24

### What's Changed
* Stop passing a GitHub token to CI to download the toolkit by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/32
* Cache Chrome's package so a restore works as a non-root user by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/34


**Full Changelog**: https://github.com/circleci-functions/setup-browser-tools/compare/v0.3.0...v0.4.0

## [0.3.0] - 2026-10-22

### What's Changed
* Update module golang.org/x/mod to v0.40.0 [SECURITY] by @renovate[bot] in https://github.com/circleci-functions/setup-browser-tools/pull/11
* Update module golang.org/x/text to v0.41.0 [SECURITY] by @renovate[bot] in https://github.com/circleci-functions/setup-browser-tools/pull/12
* Update toolkit to v0.3.0 by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/30
* Respect the Chrome channel when detecting and replacing Chrome by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/26
* Pick the right driver builds for each platform and install one chromedriver by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/27
* Check privileges and dependencies before downloading by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/28
* Lead the README with the functions block and fill its gaps by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/29

### New Contributors
* @renovate[bot] made their first contribution in https://github.com/circleci-functions/setup-browser-tools/pull/11

**Full Changelog**: https://github.com/circleci-functions/setup-browser-tools/compare/v0.2.0...v0.3.0

## [0.2.0] - 2026-10-13

### What's Changed
* Release by merging a release PR by @stiyyagura0901 in https://github.com/circleci-functions/setup-browser-tools/pull/9


**Full Changelog**: https://github.com/circleci-functions/setup-browser-tools/compare/v0.1.0...v0.2.0

## [0.1.0] - 2026-10-07

The last release made by pushing a tag, published as `v0.1.0-c64adc5`. Earlier releases are in
the repo's [tags](https://github.com/circleci-functions/setup-browser-tools/tags).
