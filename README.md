# MCPDashboard

**Version 1.0.0**

MCPDashboard is a native desktop app for managing Model Context Protocol (MCP) server registrations across Codex, Claude, Gemini CLI, Cursor, and VS Code. It brings local client configuration, authentication setup, connection tests, and reusable profiles into one English-language interface with a clean light theme.

## Contents

- [Install and run](#install-and-run)
- [Client support](#client-support)
- [Add your first server](#add-your-first-server)
- [Manage registrations and authentication](#manage-registrations-and-authentication)
- [Export and import profiles](#export-and-import-profiles)
- [Command-line import and export](#command-line-import-and-export)
- [Settings files, backups, and recovery](#settings-files-backups-and-recovery)
- [Troubleshooting](#troubleshooting)
- [Build, test, and contribute](#build-test-and-contribute)
- [MCPD file format](#mcpd-file-format)
- [License](#license)

## Install and run

Download the archive for your platform from the [latest release](https://github.com/HamedFathi/MCPDashboard/releases/latest) and extract it. If you are working from a source checkout, follow [Build, test, and contribute](#build-test-and-contribute).

| Platform | Release asset | Requirements |
| --- | --- | --- |
| Windows x64 | `MCPDashboard-<version>-windows-amd64.zip` | Windows x64 with a desktop session. The build opens a diagnostics console. |
| Windows ARM64 | `MCPDashboard-<version>-windows-arm64.zip` | Windows 11 on ARM. Fyne needs OpenGL, which Windows on ARM provides through Microsoft's OpenCL, OpenGL, and Vulkan Compatibility Pack. |
| Linux x64 | `MCPDashboard-<version>-linux-amd64.tar.gz` | Ubuntu 24.04 or a compatible Linux desktop with X11 or XWayland and OpenGL. |
| Linux ARM64 | `MCPDashboard-<version>-linux-arm64.tar.gz` | Same as Linux x64, on ARM64 hardware. |
| macOS 13+ (Apple silicon) | `MCPDashboard-<version>-macos-arm64.zip` | Contains an ad-hoc signed `MCPDashboard.app`. |
| macOS 13+ (Intel) | `MCPDashboard-<version>-macos-amd64.zip` | Contains an ad-hoc signed `MCPDashboard.app`. |

Each release includes a `SHA256SUMS` file for verifying the downloaded archives.

The desktop executables include the Go runtime. Install any MCP clients and launcher runtimes you plan to use separately. A launcher such as `npx` or `uvx` must be available to the client that starts the server.

The Mac bundles are ad-hoc signed and have yet to be notarized for public distribution. A Mac can build and sign a local development copy with `sh scripts/package-macos.sh`. See [Apple's notarization documentation](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution).

MCPDashboard retains the application's earlier data directory, ID, and profile schema. Users upgrading from MCP Manager can reuse their workspace selection, paused registrations, backups, and JSON exports.

## Client support

| Client | Local configuration scope | Transports | Authentication in this app |
| --- | --- | --- | --- |
| Codex | User, workspace | stdio, Streamable HTTP | Environment-backed Bearer and headers, forwarded variables, client OAuth commands |
| Claude Code | User, workspace | stdio, Streamable HTTP, legacy SSE | Environment-backed Bearer and headers, forwarded variables, client OAuth commands |
| Claude Desktop | User | stdio | A local launcher can read inherited environment variables |
| Gemini CLI | User, workspace | stdio, Streamable HTTP, legacy SSE | Forwarded variables for stdio and guidance for client-side HTTP auth |
| Cursor | User, workspace | stdio, Streamable HTTP, legacy SSE | Environment-backed Bearer and headers, forwarded variables, client-side OAuth guidance |
| VS Code | Default user profile, workspace | stdio, Streamable HTTP, legacy SSE | Environment-backed Bearer and headers, forwarded variables, client-side OAuth guidance |

The **Hosted connections** tab prepares the HTTPS endpoint and opens setup for ChatGPT / Work and Claude web / Cowork. Complete registration, sign-in, and authorization in the provider account. This tab also links to Codex Cloud and Gemini setup resources. Workspace policies, installed extensions, VS Code profiles other than the default, and hosted accounts are managed by their respective clients.

## Add your first server

Close affected clients before changing their configuration. Use **Choose workspace** if you want a project-scoped registration or need Claude Code's per-project pause setting.

### Remote example · Microsoft Learn

Open **Local registrations → Add server** and fill the form.

| Field | Value |
| --- | --- |
| Server name | `microsoft-learn` |
| Transport | **Streamable HTTP** |
| Server URL | `https://learn.microsoft.com/api/mcp` |
| Bearer variable and Header references | Leave blank |
| Destinations | Select Codex, Claude Code, Gemini CLI, Cursor, or VS Code as needed |

Save the registration, select its row, and use **Test HTTP**. Reopen the client and try asking it to find a page using Microsoft Learn. [Microsoft Learn MCP server reference](https://learn.microsoft.com/en-us/training/support/mcp-developer-reference).

### Local example · Playwright

Choose **Local command (stdio)**, name the server `playwright`, and enter `npx` in **Executable**. Put each argument on its own line.

```text
-y
@playwright/mcp@latest
```

Select the destinations and save. Keep paths with spaces on one argument line. The target client runs the launcher and may download the package on first use. [Playwright MCP](https://github.com/microsoft/playwright-mcp).

### Browse the registry

Open **MCP Registry** to search the official catalog. Select an entry and click **Review and add**. The form can prefill an HTTP or SSE endpoint or a standard npm or PyPI launcher. Review the publisher and any required arguments, runtime, and environment variables before saving. [Official MCP Registry](https://registry.modelcontextprotocol.io/).

## Manage registrations and authentication

- **Edit connection** updates the URL or local launcher. When a connection changes, clearing its existing auth fields requires confirmation.
- **Edit auth** accepts variable names such as `MCP_TOKEN` or header mappings such as `X-API-Key=MCP_API_KEY`. Set the values in the target client's launch environment. **OAuth setup** delegates to the target client or provides its setup command.
- **Disable / pause** uses Codex's native flag and Claude Code's per-project user toggle. For a Claude Code user registration, **Pause in workspace…** prompts for a folder if no workspace is selected. Other supported scopes park the definition in local app data and restore it on resume.
- **Test HTTP** performs one MCP initialization request using configured headers and environment values available to MCPDashboard. OAuth sessions belong to the client.
- **Remove** removes the registration from the selected target. The client and package launcher manage OAuth credentials, processes, and downloaded packages.

The dashboard shows configuration state. For live tool availability, check the target client's MCP panel. Local processes are started by the target client. Hosted servers are operated by their providers.

## Export and import profiles

### Export

1. Select a workspace first if you want its entries included.
2. Open **Profiles → Export MCP profile** and choose a folder.
3. Keep the generated `mcpdashboard-profile-*.mcpd` file for reuse.

A profile contains MCP definitions from supported user scopes and the selected workspace. It retains paused registrations, enabled states, the selected workspace's Claude Code disable list, VS Code MCP inputs, and Gemini CLI MCP options when present. It preserves native TOML values and can retain absolute paths inside launcher definitions.

### Import

1. On the destination machine, choose a workspace if the profile contains workspace entries or Claude Code workspace disable states.
2. Open **Profiles → Open MCP profile** and select the `.mcpd` file.
3. Review the destinations and registrations, then click **Import profile**.

An existing registration with the same name at the same destination is kept. Repeated imports do not create duplicates. Existing VS Code input IDs are preserved. Gemini policy is restored when the destination has no existing registrations or policy. Older MCPDashboard `.json` exports can be imported directly and exported again as `.mcpd`.

The [Microsoft Learn example profile](MCPDashboard/examples/microsoft-learn.mcpd) contains two public endpoint registrations, one for Codex and one for VS Code. It contains no credentials.

The file can contain credentials already present in configuration. Keep it private and import profiles from sources you trust. The checksum detects damage. The file can be decoded by anyone who has it, and a profile does not authenticate its publisher. OAuth session stores and unrelated client settings stay in the clients. Absolute launcher paths need to work on the destination machine.

## Command-line import and export

Use the same executable as the desktop app. Running it without arguments opens the UI. Running `import` or `export` performs the operation in the terminal without opening a window or waiting for input.

### Windows PowerShell

```powershell
$app = ".\MCPDashboard.exe"
& $app export --output ".\backup.mcpd" --overwrite
& $app import --input ".\backup.mcpd" --dry-run
& $app import --input ".\backup.mcpd"
```

### Linux and macOS

Set `app` to the path of your executable.

```sh
# Linux
app=./MCPDashboard
# macOS
# app=./MCPDashboard.app/Contents/MacOS/MCPDashboard

"$app" export --output ./backup.mcpd --workspace "/path/to/project"
"$app" import --input ./backup.mcpd --workspace "/path/to/target project" --dry-run
"$app" import --input ./backup.mcpd --workspace "/path/to/target project"
```

| Option | Behavior |
| --- | --- |
| `export --output FILE.mcpd` | Write a binary profile to an explicit path. Its parent directory must exist. |
| `export --overwrite` | Replace an existing regular output file atomically. Without this flag, an existing file is kept and the command fails. |
| `import --input FILE` | Read an `.mcpd` or legacy `.json` profile. Keep existing registrations with the same name and destination. |
| `import --dry-run` | Check the profile and destination settings and report the changes without writing. The result reflects configuration at the time of the check. |
| `--workspace DIR` | Use the specified project folder. Export without this flag includes user scopes only. Import requires it for workspace entries or Claude Code workspace disable states. |

The CLI uses the same user configuration paths, backup behavior, and profile format as the UI. It uses the workspace supplied on the command line, independently of the desktop selection. A successful command emits one JSON object to stdout. Export returns `file` and `destinations`. Import returns `file`, `dry_run`, `added`, `paused`, `kept`, and `settings`. Errors go to stderr. The exit status is `0` for success, `1` for an operation failure, or `2` for invalid arguments. Use `--help` for the command syntax.

## Settings files, backups, and recovery

Common user configuration locations are listed below. Select a registration in the dashboard to see its exact file path.

| Client | User configuration |
| --- | --- |
| Codex | `~/.codex/config.toml` or `config.toml` under `CODEX_HOME` |
| Claude Code | `~/.claude.json` or `.claude.json` under `CLAUDE_CONFIG_DIR` |
| Claude Desktop | `claude_desktop_config.json` in the platform's Claude application-data folder |
| Gemini CLI | `~/.gemini/settings.json` or `.gemini/settings.json` under `GEMINI_CLI_HOME` |
| Cursor | `~/.cursor/mcp.json` |
| VS Code | `mcp.json` in the platform's default VS Code user profile |

- A missing settings file and its parent directory are created when you save. Blank, whitespace-only, BOM-only, and JSONC comment-only files are initialized as needed. Valid BOM-marked UTF-16 files are read and saved as UTF-8.
- For invalid syntax, the editor identifies the destination and shows the line and column when available. **Back up and recover** saves the exact original bytes first. Recovery replaces only the damaged MCP section when the surrounding file is readable. Recovery of an unreadable file starts a fresh configuration containing the requested registration.
- **Skip and save others** keeps the affected file untouched and applies the registration to the remaining selected destinations.
- Backups live under `MCP Manager/backups` and explicit recovery copies under `MCP Manager/recovery` in the operating system's user configuration directory. The app shows the recovery copy path. To restore a backup, close the target client and the dashboard, then copy it over the original settings file.

Configuration changes are preflighted before a multi-destination write. If a later write fails, the app attempts to roll back earlier writes. An interruption between separate file writes can leave a partially applied operation. Unrelated settings are preserved, and JSONC comments outside changed values are retained. Rewriting Codex TOML can change its formatting and comments.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| A server is registered but missing from the client | Reopen the client and check its selected workspace, profile, and trust or policy settings. |
| A local launcher cannot start | Check that the target client's environment can find the executable and that all arguments are on separate lines. Use an absolute executable path when needed. |
| An HTTP test returns 401 or 403 | Verify the server's required header, variable value, permissions, or OAuth login in the target client. |
| Pause is workspace-scoped in Claude Code | Select **Pause in workspace…** and choose the folder used by Claude Code. |
| A profile fails its signature or checksum check | Use an intact `.mcpd` export. Keep a legacy export's `.json` extension so the import dialog reads its original format. |
| Import requests a workspace | Choose the target workspace from **Local registrations**, then reopen the profile. |

When reporting an issue, include the operating system, app version, affected client, and exact error message. Remove tokens, auth headers, private paths, and credential-bearing launcher arguments from any shared configuration.

## Build, test, and contribute

Install Go 1.27.3 or newer and the desktop build dependencies for your OS. The Go module lives in the `MCPDashboard` folder, so run the commands there. Fyne needs CGO and a C compiler on every platform.

### Linux

On Debian or Ubuntu, install `gcc`, `pkg-config`, `libgl1-mesa-dev`, and `xorg-dev`.

```sh
go build -tags x11 -trimpath -o MCPDashboard .
./MCPDashboard --version
./MCPDashboard
```

### Windows

Install MinGW-w64 GCC, for example through [MSYS2](https://www.msys2.org/) with `pacman -S mingw-w64-x86_64-gcc`. In PowerShell, put it on PATH and build the same way the release workflow does.

```powershell
cd MCPDashboard
$env:PATH = "C:\msys64\mingw64\bin;$env:PATH"
$env:CGO_ENABLED = "1"
go build -buildvcs=false -trimpath -ldflags='-s -w' -o ..\dist\MCPDashboard.exe .
..\dist\MCPDashboard.exe --version
```

Double-click `dist\MCPDashboard.exe` to start the desktop app. The `dist/` folder is ignored by Git. Without a C compiler, the build fails with `build constraints exclude all Go files` in `go-gl` or `C compiler "gcc" not found`.

The tracked `release_windows_amd64.syso` embeds the version resource and can be regenerated from `packaging/MCPDashboard.rc` with MinGW-w64 `windres`.

### macOS

Install Xcode command-line tools. Build a native executable or create a universal `.app` for local testing.

```sh
go build -trimpath -o MCPDashboard .
./MCPDashboard --version

sh scripts/package-macos.sh
```

The packaging script builds both architectures, creates `dist/macos/MCPDashboard.app`, applies an ad-hoc signature, and verifies it with Apple tools.

### Tests and CI

```sh
go test -tags ci -shuffle=on ./...
go vet -tags ci ./...
go test -race -tags ci -shuffle=on -count=5 ./...
```

The `ci` tag uses the Fyne software driver so tests can exercise the UI without a display. Race testing requires CGO. A live test of the public Microsoft Learn endpoint is opt-in.

```sh
MCPD_LIVE_SMOKE=1 go test -tags ci -run '^TestMicrosoftLearnLive$' -v
```

GitHub Actions is configured to test and build on Linux, Windows x64, and macOS on Apple silicon and Intel. Keep platform-specific changes covered by tests and use temporary configuration files for test data. The Linux executable passed a terminal smoke test that imported, exported, replaced, and previewed profiles with no display. The Windows and Mac builds have build and metadata checks. Native Windows and macOS runtime validation and Apple notarization for public Mac distribution are release gates. Run the hosted checks when the repository is available.

### Publish a release

Releases are built and published by `.github/workflows/release.yml`.

1. Set the new version in `release.go`, `FyneApp.toml`, `packaging/Info.plist`, `packaging/MCPDashboard.rc`, and the version assertions in the tests, then regenerate `release_windows_amd64.syso`.
2. Commit, then tag and push: `git tag v1.2.0 && git push origin v1.2.0`.

The workflow refuses to publish if the tag does not match those files. It tests and builds Windows x64 and ARM64, Linux x64 and ARM64, and macOS Apple silicon and Intel, then creates the GitHub Release with generated notes, the archives, and `SHA256SUMS`. To rebuild an existing tag, run the workflow manually and enter the tag.

### Repository layout

| Path (under `MCPDashboard/`) | Purpose |
| --- | --- |
| `main.go`, `editor.go`, `panels.go`, `profile_ui.go` | Desktop screens and actions |
| `theme.go` | Light color palette, sizes, and shared layout helpers |
| `targets.go`, `config.go`, `manager.go` | Client configuration paths, file handling, and registration operations |
| `profile.go`, `profile_file.go` | Profile planning and the MCPD binary format |
| `profile_cli.go` | Headless import and export in the desktop executable |
| `network.go`, `oauth.go` | Registry discovery, HTTP probe, and client OAuth handoff |
| `packaging/`, `scripts/`, `FyneApp.toml` | Version metadata and platform packaging |
| `*_test.go`, `../.github/workflows/` | Regression tests, CI, and release builds |
| `examples/` | Importable sample MCPD profile |
| `../dist/`, `dist/` | Local build output, ignored in source control |

## MCPD file format

An `.mcpd` profile is a binary container with a 52-byte header and one zlib-compressed payload. Numeric header fields use big-endian byte order. The payload encodes the UTF-8 JSON profile schema `mcp-manager-profile/v1`, including the native TOML or JSON configuration of each destination.

| Byte offset | Size | Meaning |
| --- | --- | --- |
| 0 | 8 | Magic bytes `4D 43 50 44 0D 0A 1A 0A` |
| 8 | 2 | Container version, currently `1` |
| 10 | 2 | Reserved flags, currently `0` |
| 12 | 4 | Compressed payload length |
| 16 | 4 | Decoded payload length |
| 20 | 32 | SHA-256 of the compressed payload |
| 52 | Variable | One complete zlib stream |

The importer requires a matching signature, supported version and flags, consistent lengths, matching checksum, and a valid profile schema. The decoded payload is limited to 32 MiB, and the compressed payload to 32 MiB plus 64 KiB. Extra data outside or inside the compressed stream is rejected. The extension alone does not identify a valid file.

## Screenshots

<img width="1125" height="773" alt="Screenshot_1" src="https://github.com/user-attachments/assets/8909fe0d-1b26-4cbf-b5c3-4540d7b4567b" />

<img width="942" height="791" alt="Screenshot_2" src="https://github.com/user-attachments/assets/1cb522ef-dbfc-44b0-853a-9402c1e336a0" />

<img width="1122" height="774" alt="Screenshot_3" src="https://github.com/user-attachments/assets/4423961e-2d4a-466e-9304-9e6d18650126" />

<img width="1119" height="773" alt="Screenshot_4" src="https://github.com/user-attachments/assets/9a726f57-6a76-4722-b0a2-6dc22c1cde74" />

<img width="1121" height="774" alt="Screenshot_5" src="https://github.com/user-attachments/assets/7c12b74a-312c-4867-8819-1495a1325893" />

## License

MCPDashboard is distributed under the [MIT License](LICENSE). Dependency and runtime notices are in [THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt).
