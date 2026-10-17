## Contributing to Docker Cleaner

Thank you for your interest! Here are some guidelines to help you get started:

### How to contribute
- Fork the repository and clone locally.
- Create a new branch for your feature or bugfix.
- Write commit messages following the convention below.
- Open a pull request describing your changes.

### Commit messages
We use [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): summary`, where the scope is optional.

| Type | Use for |
|---|---|
| `feat` | A new feature |
| `fix` | A bug fix |
| `refactor` | A code change that neither fixes a bug nor adds a feature |
| `docs` | Documentation only |
| `test` | Adding or fixing tests |
| `chore` | Build, CI, dependencies and other housekeeping |

For example: `feat: add --version flag` or `fix(docker): keep image order stable`.
The release notes are generated from these messages: `feat` and `fix` commits get their own sections, while `docs`, `test` and `chore` commits are left out.

### Coding Style
- Use GoDoc comments for exported functions and types.
- Run `go fmt ./...` before submitting.

### Testing
- Add unit tests for new features using the `testing` package.
- Run `go test ./...` and ensure all tests pass.

### Releases
Releases are cut by tagging a commit on `main` as `vX.Y.Z` and pushing the tag, e.g. `git tag v0.2.0 && git push origin v0.2.0`.
The [release workflow](.github/workflows/release.yml) then runs [GoReleaser](https://goreleaser.com), which publishes the binaries and changelog to GitHub Releases and updates the Homebrew tap.

### Reporting Issues
- Please provide steps to reproduce, expected behavior, and screenshots if applicable.
