## Docker Cleaner

This is a terminal user interface application built with Go and Bubble Tea for cleaning up Docker images and containers.

### Features

- List all Docker images
- List all Docker containers (running and stopped)
- Select multiple images/containers for deletion
- Delete selected images/containers

### Prerequisites

- Go (version 1.22 or higher)
- Docker (running on your system)

### Installation

1. Clone the repository:

```bash
git clone https://github.com/minhajul/docker-cleaner.git
cd docker-cleaner
```

### Usage

1. Build the application: 
   ```bash
   make build
   ```
2. Run the application:
   ```bash
   make run
   ```
3. Select what to remove and press `d`. Nothing is deleted until you confirm with `y`.

#### Keys

| Key | Action |
|---|---|
| `↑`/`k`, `↓`/`j` | Move the cursor |
| `pgup`/`pgdown`, `g`/`G` | Page up/down, jump to first/last row |
| `space`/`enter` | Select an item; on a section header, select or clear the whole section |
| `a` / `n` | Select all / select none |
| `r` | Refresh the lists |
| `d` | Delete selected (asks for confirmation) |
| `y` / `f` / `n` | In the confirmation: delete / force-delete running containers / cancel |
| `f` | After a cleanup: force-remove images that failed because they are in use or have several tags |
| `?` | Show all keys |
| `q`, `ctrl+c` | Quit |

![Screenshot](art/screenshot.png)

### Development

To run tests:

```bash
make test
```

To run linter:

```bash
make lint
```

To clean up:

```bash
make clean
```

### Contributing

Feel free to open issues or pull requests if you have any suggestions or improvements.
See [CONTRIBUTING.md](CONTRIBUTING.md).

### Made with ❤️ by [[minhajul](https://github.com/minhajul)]