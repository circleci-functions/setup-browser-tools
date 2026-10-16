# setup-browser-tools

Installs browsers and browser-testing tools into a CircleCI job: Google Chrome, Chrome for Testing, ChromeDriver, Microsoft Edge and its WebDriver, Firefox, and geckodriver. Each is a subcommand, and `install-browser-tools` installs a default set in one step.

Invoked via:

```
circleci run github.com/circleci-functions/setup-browser-tools@<version> -- <subcommand> [flags]
```

The full `github.com/` path and the `--` separator are both required. A bare
`setup-browser-tools@<version>` does not resolve.

## Capabilities

- **Installs a default set** — Chrome, ChromeDriver, Firefox and geckodriver — with one command
- **Installs individually**, so a job can take only what it needs
- **Pins versions**, or resolves the latest release from the vendor
- **Verifies downloads**: SHA-256/512 checksums for Firefox, plus OpenPGP signature verification of the checksum manifest
- **Caches installers** for Chrome and Firefox via the CircleCI job cache, for when downloads are unreliable
- **Records what it installed** in `$HOME/.browser-versions`, one line per browser
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

## Flags

### `install-browser-tools`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--chrome` / `--no-chrome` | bool | `true` | Install Google Chrome |
| `--chromedriver` / `--no-chromedriver` | bool | `true` | Install ChromeDriver |
| `--firefox` / `--no-firefox` | bool | `true` | Install Firefox |
| `--geckodriver` / `--no-geckodriver` | bool | `true` | Install geckodriver |
| `--chrome-for-testing` | bool | `false` | Install Chrome for Testing |
| `--chrome-for-testing-driver` | bool | `false` | Install the Chrome for Testing ChromeDriver alongside it |
| `--edge` | bool | `false` | Install Microsoft Edge |
| `--edge-driver` | bool | `false` | Install Edge WebDriver |
| `--chrome-version` | string | `latest` | Version of Chrome to install |
| `--replace-existing-chrome` | bool | `false` | Replace an existing Chrome when it is a different version |
| `--chromedriver-install-dir` | string | `/usr/local/bin` | Directory to install ChromeDriver into |
| `--chrome-for-testing-version` | string | `latest` | Version of Chrome for Testing to install |
| `--chrome-for-testing-install-dir` | string | `/usr/local/bin` | Directory to symlink Chrome for Testing and its driver into |
| `--firefox-version` | string | `latest` | Version of Firefox to install |
| `--firefox-install-dir` | string | `/usr/local/bin` | Directory to install Firefox into |
| `--geckodriver-version` | string | `latest` | Release tag of geckodriver to install |
| `--geckodriver-install-dir` | string | `/usr/local/bin` | Directory to install geckodriver into |
| `--edge-version` | string | `latest` | Version of Edge to install |
| `--edge-driver-install-dir` | string | `/usr/local/bin` | Directory to install Edge WebDriver into |

### `install-chrome`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--version` | string | `latest` | Version to install, e.g. `147.0.7727.55` |
| `--channel` | enum | `stable` | Release channel: `stable` or `beta` |
| `--replace-existing` | bool | `false` | Replace an existing Chrome when it is a different version |
| `--use-cache` | bool | `false` | Restore and save Chrome via the job cache |
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
| `--install-dir` | string | `/usr/local/bin` | Directory to symlink Chrome and its driver into |
| `--chromedriver` / `--no-chromedriver` | bool | `true` | Also install the matching ChromeDriver |

### `install-edge`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--version` | string | `latest` | Version to install. Only honoured on Debian/Ubuntu; macOS always installs the latest. |

### `install-edge-driver`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--install-dir` | string | `/usr/local/bin` | Directory to install into |

## Version resolution

`latest` resolves the current stable release from the vendor. Any other value is installed as given.

A version may reference an environment variable, as `$NAME` or `${NAME}`. A reference to a variable that is unset, or set to the empty string, is an error rather than an empty version — so a typo fails immediately instead of building a broken download URL.

`install-chromedriver` and `install-edge-driver` pick the driver for the installed browser. When none was published for that exact build, ChromeDriver falls back to the newest driver for the same milestone, and Edge WebDriver to the newest for the same major version.

## Job cache

`install-chrome` and `install-firefox` accept `--use-cache`, which stores the installer in the CircleCI job cache and restores it on later runs. It increases cost and rarely speeds things up, so it is off by default; reach for it when downloads from a vendor mirror are unreliable.

Keys are:

```
setup-browser-tools-chrome-<cache-key>-<channel>-<version>-<os>-<arch>
setup-browser-tools-firefox-<cache-key>-<version>-<os>-<arch>
```

`--cache-key` (default `v1`) is the prefix to bump when an entry needs invalidating. Firefox is not cached when `--version` is `latest`, since the key would pin whichever version it first resolved. A restored Firefox installer is checksum- and signature-verified exactly as a fresh download is.

Cache failures are never fatal: a cache miss, an unreachable cache, or running outside a CircleCI job all fall through to a normal install.

## Setting a timeout

There is no timeout flag. Set `no_output_timeout` on the step that runs the function:

```yaml
- run:
    name: Install Chrome
    command: circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-chrome
    no_output_timeout: 5m
```

## Preconditions

- Root, or `sudo` available — browsers install into system directories
- `gpg` on `PATH` for `install-firefox`, which verifies the checksum manifest's signature
- `brew` on `PATH` for `install-edge` on macOS
- `install-chromedriver` needs Chrome already installed; `install-edge-driver` needs Edge

## Examples

```yaml
jobs:
  test:
    docker:
      - image: cimg/base:current
    steps:
      # Chrome, ChromeDriver, Firefox and geckodriver
      - run: circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-browser-tools

      # Chrome only
      - run: circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-chrome

      # Pinned versions
      - run: |
          circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-browser-tools \
            --chrome-version 147.0.7727.55 \
            --firefox-version 149.0.2

      # Swap Firefox and geckodriver for Edge and its driver
      - run: |
          circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-browser-tools \
            --no-firefox --no-geckodriver --edge --edge-driver

      # Chrome from the beta channel, with the download cached
      - run: |
          circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-chrome \
            --channel beta --use-cache --cache-key v2

      # Chrome for Testing and its driver, into a chosen directory
      - run: |
          circleci run github.com/circleci-functions/setup-browser-tools@<version> -- install-chrome-for-testing \
            --version 147.0.7727.55 --install-dir /home/circleci/bin
```

## Platform support

Coverage varies by browser, because the vendors publish different builds.

| | macOS | Debian/Ubuntu | Alpine | RHEL/CentOS | Linux ARM64 | Windows |
|---|---|---|---|---|---|---|
| `install-chrome` | yes | yes | yes, as Chromium | yes | Alpine only, as Chromium | no |
| `install-chromedriver` | yes | yes | yes | `unzip` only | Alpine only | no |
| `install-firefox` | yes | yes | no | yes | no | no |
| `install-geckodriver` | yes | yes | yes | yes | yes | no |
| `install-chrome-for-testing` | yes | yes | no | no | no | no |
| `install-edge` | yes | yes | no | no | no | no |
| `install-edge-driver` | yes | yes | no | no | no | no |

Asking for a browser on a platform it is not published for is an error. The exception is `install-browser-tools`: a component that is on only by default is warned about and skipped, so a bare run on Alpine installs Chromium, ChromeDriver and geckodriver, and reports why it skipped Firefox. Naming that component explicitly makes it an error instead.
