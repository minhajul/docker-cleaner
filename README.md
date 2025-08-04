### Docker Cleaner TUI

This is a terminal user interface (TUI) application built with Go and Bubble Tea for cleaning up Docker images and containers.

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
1. Build the application: ```go build -o docker-cleaner```
2. Run the application: ```./docker-cleaner```
2. Navigate the list using the `up` and `down` arrow keys or `k` and `j`.
3. Select/deselect items for cleanup by pressing the space bar.
4. Press d to delete all selected images and containers.
5. Press `q` or `Ctrl+C` to quit the application.

### Contributing

Feel free to open issues or pull requests if you have any suggestions or improvements.

### Made with ❤️ by [[minhajul](https://github.com/minhajul)]