# setup-browser-tools

Installs browsers and browser-testing tools into a CircleCI job: Google Chrome, Chrome for Testing, ChromeDriver, Microsoft Edge and its WebDriver, Firefox, and geckodriver. Each is a subcommand, and `install-browser-tools` installs a default set in one step. `start-xvfb` gives headed browser tests on Linux a virtual display.

## Usage

Declare the function in a top-level `functions:` block, then call a subcommand
as a step named `<alias>/<subcommand>`. Each flag below is a `with:` key,
written without its leading `--`, and a step with no flags needs no `with:`:

```yaml
functions:
  browser-tools: github.com/circleci-functions/setup-browser-tools@<version>

jobs:
  test:
    docker:
      - image: cimg/base:current
    steps:
      - browser-tools/install-chrome:
          with:
            version: 147.0.7727.55
```

`circleci function add setup-browser-tools --as browser-tools` writes the
`functions:` entry at the latest version. The alias is yours to choose. A
negatable flag such as `--no-firefox` is written as `firefox: false`.

Where the `functions:` block isn't available, as on CircleCI Server, run the
function by its full name. Everything after `--` is passed to the function:

```yaml
- run: circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-chrome --version 147.0.7727.55
```

The full `github.com/` path and the `--` separator are both required. A bare
`setup-browser-tools@<version>` does not resolve.

## Capabilities

- **Installs a default set** — Chrome, ChromeDriver, Firefox and geckodriver — with one command
- **Installs individually**, so a job can take only what it needs
- **Pins versions**, or resolves the latest release from the vendor
- **Verifies downloads**: SHA-256/512 checksums for Firefox, plus OpenPGP signature verification of the checksum manifest
- **Caches** the Firefox installer, and the Chrome package on Debian/Ubuntu and RHEL, via the CircleCI job cache, for when downloads are unreliable
- **Records what it installed** in `$HOME/.browser-versions`, one line per browser
- **Starts a virtual display** (Xvfb) for headed browser tests on Linux, and exports `DISPLAY` to later steps
- Reads a version from an environment variable, so `--version "$CHROME_VERSION"` works

## Subcommands

| Subcommand | Installs |
|---|---|
| `install-browser-tools` | A configurable set: Chrome, ChromeDriver, Firefox and geckodriver by default |
| `install-chrome` | Google Chrome, or Chromium on Alpine |
| `install-chromedriver` | ChromeDriver matching the installed Chrome |
| `install-firefox` | Mozilla Firefox |
| `install-geckodriver` | geckodriver |
| `install-chrome-for-testing` | Chrome for Testing, and its matching ChromeDriver |
| `install-edge` | Microsoft Edge |
| `install-edge-driver` | Edge WebDriver matching the installed Edge |
| `start-xvfb` | Xvfb, started on a virtual display for later steps |

## Flags

### `install-browser-tools`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--chrome` / `--no-chrome` | bool | `true` | Install Google Chrome |
| `--chromedriver` / `--no-chromedriver` | bool | `true` | Install ChromeDriver |
| `--firefox` / `--no-firefox` | bool | `true` | Install Firefox |
| `--geckodriver` / `--no-geckodriver` | bool | `true` | Install geckodriver |
| `--chrome-for-testing` | bool | `false` | Install Chrome for Testing |
| `--chrome-for-testing-driver` | bool | `false` | Install the Chrome for Testing ChromeDriver alongside it, in place of the default ChromeDriver. Passing `--chromedriver` as well is an error. |
| `--edge` | bool | `false` | Install Microsoft Edge |
| `--edge-driver` | bool | `false` | Install Edge WebDriver |
| `--chrome-version` | string | `latest` | Version of Chrome to install |
| `--replace-existing-chrome` | bool | `false` | Replace an existing Chrome when it is a different version. Without it, an installed Chrome is kept whatever its version. |
| `--chromedriver-install-dir` | string | `/usr/local/bin` | Directory to install ChromeDriver into |
| `--chrome-for-testing-version` | string | `latest` | Version of Chrome for Testing to install |
| `--chrome-for-testing-install-dir` | string | `/usr/local/bin` | Directory to symlink Chrome for Testing into and copy its driver into |
| `--firefox-version` | string | `latest` | Version of Firefox to install |
| `--firefox-install-dir` | string | `/usr/local/bin` | Directory to install Firefox into |
| `--geckodriver-version` | string | `latest` | Release tag of geckodriver to install |
| `--geckodriver-install-dir` | string | `/usr/local/bin` | Directory to install geckodriver into |
| `--edge-version` | string | `latest` | Version of Edge to install |
| `--edge-driver-install-dir` | string | `/usr/local/bin` | Directory to install Edge WebDriver into |
| `--xvfb` | bool | `false` | After installing, start Xvfb on `:99` and export `DISPLAY`, as `start-xvfb` does with its defaults |

### `install-chrome`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--version` | string | `latest` | Version to install, e.g. `147.0.7727.55` |
| `--channel` | enum | `stable` | Release channel: `stable` or `beta` |
| `--replace-existing` | bool | `false` | Replace an existing Chrome of the same channel when it is a different version. Without it, an installed Chrome of that channel is kept whatever its version. |
| `--use-cache` | bool | `false` | Restore and save the Chrome package via the job cache |
| `--cache-key` | string | `v1` | Cache key prefix, for invalidating cached entries |

### `install-chromedriver`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--install-dir` | string | `/usr/local/bin` | Directory to install into. Not honoured on Alpine, where the package manager chooses. |

### `install-firefox`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--version` | string | `latest` | Version to install, e.g. `149.0.2` or `140.9.1esr` |
| `--install-dir` | string | `/usr/local/bin` | Directory to install into. A symlink is also created in `/usr/local/bin`. |
| `--use-cache` | bool | `false` | Restore and save the installer via the job cache. Ignored when `--version` is `latest`. |
| `--cache-key` | string | `v1` | Cache key prefix |

### `install-geckodriver`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--version` | string | `latest` | Release tag to install, e.g. `v0.36.0` |
| `--install-dir` | string | `/usr/local/bin` | Directory to install into |

### `install-chrome-for-testing`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--version` | string | `latest` | Version to install |
| `--install-dir` | string | `/usr/local/bin` | Directory to symlink Chrome into and copy its driver into |
| `--chromedriver` / `--no-chromedriver` | bool | `true` | Also install the matching ChromeDriver |

### `install-edge`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--version` | string | `latest` | Version to install. Only honoured on Debian/Ubuntu; macOS always installs the latest. |

### `install-edge-driver`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--install-dir` | string | `/usr/local/bin` | Directory to install into |

### `start-xvfb`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--display` | string | `:99` | X display to start Xvfb on, and to export as `DISPLAY` |
| `--screen` | string | `1280x1024x24` | Geometry and colour depth of the virtual screen, as `WIDTHxHEIGHTxDEPTH` |

## Headed browsers on Linux

A browser run without `--headless` needs an X display, which a Linux job does
not have. `start-xvfb`, or `--xvfb` on `install-browser-tools`, provides one:

- Installs Xvfb when it is not already on `PATH`: `xvfb` on Debian/Ubuntu and
  Alpine, `xorg-x11-server-Xvfb` on RHEL. This needs root or `sudo`; Xvfb itself
  runs as the job user.
- Starts Xvfb on the display, unless a server is already answering there, in
  which case it is reused. Xvfb keeps running after the step ends, for the rest
  of the job, and logs to `$TMPDIR/xvfb-<display number>.log` (`/tmp` when
  `TMPDIR` is unset).
- Exports `DISPLAY` through `$BASH_ENV`, so later steps that run in bash see it.
  Without `$BASH_ENV`, which CircleCI always sets, it fails rather than starting
  a display no later step could find.

On macOS it does nothing: the job already has a display.

```yaml
steps:
  - browser-tools/install-browser-tools:
      with:
        xvfb: true
  - run: npm run test:headed  # DISPLAY is set
```

## Version resolution

`latest` resolves the current stable release from the vendor. Any other value is installed as given.

A version may reference an environment variable, as `$NAME` or `${NAME}`. A reference to a variable that is unset, or set to the empty string, is an error rather than an empty version — so a typo fails immediately instead of building a broken download URL.

`install-chromedriver` and `install-edge-driver` pick the driver for the installed browser. When none was published for that exact build, ChromeDriver falls back to the newest driver for the same milestone, and Edge WebDriver to the newest for the same major version.

## Job cache

`install-chrome` and `install-firefox` accept `--use-cache`, which saves to the CircleCI job cache and restores from it on later runs. It increases cost and rarely speeds things up, so it is off by default; reach for it when downloads from a vendor mirror are unreliable.

What is cached differs. For Firefox it is the downloaded installer. For Chrome it is the downloaded `.deb` or `.rpm`, kept under the user's cache directory, and only on Debian/Ubuntu and RHEL: on Alpine and macOS nothing is cached. A restored package is installed with apt or yum just as a fresh download is, so Chrome's shared-library dependencies come with it. With `--use-cache`, `latest` on Debian/Ubuntu installs Google's current-release `.deb` rather than adding Google's apt repository.

Keys are:

```
setup-browser-tools-chrome-<cache-key>-<channel>-<version>-<os>-<arch>
setup-browser-tools-firefox-<cache-key>-<version>-<os>-<arch>
```

`--cache-key` (default `v1`) is the prefix to bump when an entry needs invalidating. Firefox is not cached when `--version` is `latest`, since the key would pin whichever version it first resolved. A restored Firefox installer is checksum- and signature-verified exactly as a fresh download is.

Cache failures are never fatal: a cache miss, an unreachable cache, or running outside a CircleCI job all fall through to a normal install.

## Setting a timeout

There is no timeout flag, and a function step doesn't take `no_output_timeout`.
Where a timeout matters, run the function by its full name in a `run:` step and
set it there:

```yaml
- run:
    name: Install Chrome
    command: circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-chrome
    no_output_timeout: 5m
```

## Preconditions

- Root, or `sudo` available — browsers install into system directories
- `install-firefox` verifies the checksum manifest's signature with `gpg`. On Debian/Ubuntu and RHEL a missing `gpg` is installed along with Firefox's other dependencies (`gnupg` or `gnupg2`); on macOS it comes from `brew`
- `brew` on `PATH` for `install-edge` on macOS
- `install-chromedriver` needs Chrome already installed; `install-edge-driver` needs Edge
- `start-xvfb` and `--xvfb` need `$BASH_ENV`, which CircleCI sets in every job, to pass `DISPLAY` on

Without root or `sudo`, a subcommand that would run the package manager or write to a directory the job user can't write fails before downloading anything, and says which. The drivers (`install-chromedriver` outside Alpine, `install-geckodriver`, `install-edge-driver`) need neither when `--install-dir` is writable.

## Examples

```yaml
functions:
  browser-tools: github.com/circleci-functions/setup-browser-tools@<version>

jobs:
  test:
    docker:
      - image: cimg/base:current
    steps:
      # Chrome, ChromeDriver, Firefox and geckodriver
      - browser-tools/install-browser-tools

      # Chrome only
      - browser-tools/install-chrome

      # Pinned versions
      - browser-tools/install-browser-tools:
          with:
            chrome-version: 147.0.7727.55
            firefox-version: 149.0.2

      # Swap Firefox and geckodriver for Edge and its driver
      - browser-tools/install-browser-tools:
          with:
            firefox: false
            geckodriver: false
            edge: true
            edge-driver: true

      # Chrome from the beta channel, with the download cached
      - browser-tools/install-chrome:
          with:
            channel: beta
            use-cache: true
            cache-key: v2

      # Chrome for Testing and its driver, into a chosen directory
      - browser-tools/install-chrome-for-testing:
          with:
            version: 147.0.7727.55
            install-dir: /home/circleci/bin
```

## Platform support

Coverage varies by browser, because the vendors publish different builds. The distro columns are for amd64 (x86-64) hosts; the Linux ARM64 column covers every distro on arm64.

| | macOS | Debian/Ubuntu | Alpine | RHEL/CentOS | Linux ARM64 | Windows |
|---|---|---|---|---|---|---|
| `install-chrome` | yes | yes | yes, as Chromium | yes | Alpine only, as Chromium | no |
| `install-chromedriver` | yes | yes | yes | `unzip` only | Alpine only | no |
| `install-firefox` | yes | yes | no | yes | no | no |
| `install-geckodriver` | yes | yes | yes | yes | yes | no |
| `install-chrome-for-testing` | yes | yes | no | no | no | no |
| `install-edge` | yes | yes | no | no | no | no |
| `install-edge-driver` | yes | yes | no | no | no | no |
| `start-xvfb` | not needed | yes | yes | yes | yes | no |

Asking for a browser on a platform it is not published for is an error. The exception is `install-browser-tools`: a component that is on only by default is warned about and skipped, so a bare run on Alpine installs Chromium, ChromeDriver and geckodriver, and reports why it skipped Firefox. Naming that component explicitly makes it an error instead.

## Development

`task lint` and `task test` are hermetic. `task test` includes the command-line contract tests in `acceptance/`, but not the real-install tests there, which are tagged `acceptance`: they install browsers with the package manager and write under `/usr/local`, so they skip unless they run in a CircleCI job or `SETUP_BROWSER_TOOLS_ACCEPTANCE_DISPOSABLE=1` is set.

To run them locally, use a throwaway container:

```sh
task acceptance-docker                                   # every test, on cimg/base:current
task acceptance-docker RUN=TestAcceptanceGeckodriver     # a -test.run pattern
task acceptance-docker IMAGE=cimg/base:stable PLATFORM=linux/arm64
```

It cross-compiles the binary and the suite, mounts them into the container read-only, and runs the suite there against an in-memory stand-in for the job cache. `PLATFORM` defaults to `linux/amd64`, which every browser supports; on an arm64 host that runs emulated, and slowly. The image needs bash and, unless it runs as root, sudo.

In CI, the `acceptance` command in `.circleci/config.yml` runs the same cross-compiled suite against the job's real cache. A new install behaviour gets a test there and a row that runs it.
