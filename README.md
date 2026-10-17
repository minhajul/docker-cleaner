## Docker Cleaner

[![CI](https://github.com/minhajul/docker-cleaner/actions/workflows/ci.yml/badge.svg)](https://github.com/minhajul/docker-cleaner/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/minhajul/docker-cleaner)](https://github.com/minhajul/docker-cleaner/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A terminal UI for finding and removing Docker images and containers, built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

![Screenshot](art/screenshot.png)

### Features

- Lists your images and containers (running and stopped) with their sizes and state
- Hides Kubernetes, minikube, k3s and kind system images and containers so you can't break your cluster, plus anything you list in a [protect-list](#protecting-resources)
- Select items one by one, a whole section, or everything
- Asks before deleting, shows how much space will be freed, and warns about running containers
- Reports each failure with its reason, and offers a forced retry for images that are in use or have several tags
- Works with Docker Desktop, Colima, OrbStack, Rancher Desktop and Podman through your current Docker context

### Installation

**Homebrew** (macOS and Linux):

```bash
brew install --cask minhajul/tap/docker-cleaner
```

**Go** (1.26 or later):

```bash
go install github.com/minhajul/docker-cleaner@latest
```

**Binaries:** download an archive for macOS, Linux or Windows from the [releases page](https://github.com/minhajul/docker-cleaner/releases).

### Usage

Make sure Docker is running, then:

```bash
docker-cleaner
```

Select what to remove and press `d`. Nothing is deleted until you confirm with `y`.

docker-cleaner connects to the same daemon as the `docker` CLI: `DOCKER_HOST` if it is set, otherwise the context named by `DOCKER_CONTEXT`, otherwise your current context (`docker context use`).

```text
docker-cleaner --version         Print the version
docker-cleaner --help            Show flags and keys
docker-cleaner --config <path>   Use another config file
```

#### Keys

| Key | Action |
|---|---|
| `↑`/`k`, `↓`/`j` | Move the cursor |
| `pgup`/`pgdown`, `g`/`G` | Page up/down, jump to first/last row |
| `space`/`enter` | Select an item; on a section header, select or clear the whole section |
| `a` / `n` | Select all / select none |
| `r` | Refresh the lists |
| `d` | Delete selected (asks for confirmation) |
| `y` / `f` / `n` | In the confirmation: delete / force-delete running or paused containers / cancel |
| `f` | After a cleanup: force-remove images that failed because they are in use or have several tags |
| `p` | Show or hide protected items (listed read-only, never selected) |
| `?` | Show all keys |
| `q`, `ctrl+c` | Quit |

### Protecting resources

Images and containers you never want to delete can be listed in a config file at `~/.config/docker-cleaner/config.yaml` (or `$XDG_CONFIG_HOME/docker-cleaner/config.yaml` if `XDG_CONFIG_HOME` is set; macOS uses the same path). Pass `--config <path>` to use another file. The file is optional.

```yaml
# Resources matching any pattern are hidden and can't be deleted.
protect:
  - "postgres:*"        # image repo:tag glob
  - "my-db"             # container name
  - "prod-*"
```

Protected resources, including the built-in Kubernetes ones, are hidden; a line under the list counts them (`🔒 3 protected items hidden · press p to show`). Press `p` to list them dimmed with a 🔒 and the reason (`system` or `protect-list`). They can't be selected, so select all and delete never touch them.

Matching rules:

- Patterns are globs: `*` matches any run of characters, `?` one character, `[a-z]` a set; `\` escapes. Unlike file-name globs, `*` and `?` also match `/`.
- A pattern must match a whole name, and case matters.
- Every pattern is checked against every resource:
  - an **image** by each tag and each tag's repository, so `postgres` and `postgres:*` protect every `postgres` tag, while `postgres:16` protects only that tag. Untagged (`<none>`) images match nothing.
  - a **container** by its name, without Docker's leading `/` (a leading `/` on a pattern is ignored too). A container is not protected by its image's name, but the image a protected container runs on is protected too, so it can't be untagged from under it.

| Pattern | Matches | Doesn't match |
|---|---|---|
| `postgres` | image `postgres:16`, container `postgres` | `postgres-exporter:1`, container `my-postgres` |
| `postgres:1?` | `postgres:15`, `postgres:16` | `postgres:9`, `postgres:16.4` |
| `ghcr.io/acme/*` | `ghcr.io/acme/api:1.0`, `ghcr.io/acme/tools/cli:2` | `ghcr.io/other/api:1.0` |
| `prod-*` | container `prod-web-1`, image `prod-api:2.1` | `staging-prod-web` |

A malformed file, an unknown key (such as a misspelt `protect`) or an invalid pattern stops docker-cleaner with an error naming the file.

### Development

Requires Go 1.26+ and a running Docker daemon to try the app.

```bash
git clone https://github.com/minhajul/docker-cleaner.git
cd docker-cleaner
make run    # build and start the app
make test   # tests with the race detector
make ci     # format check, vet, tests, govulncheck and golangci-lint
```

Run `make help` to list all targets. The [roadmap](docs/ROADMAP.md) lists planned work.

### Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

### License

[MIT](LICENSE). Made with ❤️ by [minhajul](https://github.com/minhajul).
