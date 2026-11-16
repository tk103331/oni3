# oni3 (npm)

Installs the [Oni3](https://github.com/tk103331/oni3) binary for your platform
and exposes the `oni3` command.

```bash
npm install -g @tk103331/oni3

oni3                 # interactive TUI (last agent / built-in agent)
oni3 agent --agent minimax_agent
oni3 acp --agent minimax_agent
```

Supported platforms are macOS, Linux and Windows on `x64` and `arm64`. The
platform binary is downloaded from the GitHub release matching this package
version on first install (or first run). Homebrew and `go install` are also
supported.
