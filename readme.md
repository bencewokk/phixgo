# PHIX 

## Controls

- **Left Mouse Button**: Create a new ball at the cursor position. The ball's radius is determined by scrolling the mouse wheel.
- **Shift + Left Mouse Button**: Delete balls near the cursor position.
- **Right Mouse Button**: Move balls away from the cursor position.
- **Shift + Right Mouse Button**: Attract balls toward the cursor position.
- **Mouse Wheel**: Adjust the radius of the balls (scroll up to increase, scroll down to decrease).
- **Ctrl + S**: Save the current scene to `phixgo-scene.json`.
- **Ctrl + O**: Load the scene from `phixgo-scene.json`.
- **Ctrl + 1..9**: Load from a slot file (`phixgo-scene-<n>.json`).
- **Ctrl + Shift + 1..9**: Save to a slot file (`phixgo-scene-<n>.json`).

## How to run

- You will need a golang compiler (it was written in go 1.23.1 but it should work with everything else)
- Just run ```go run .```

## Self-Update

The application supports automatic updates from GitHub releases. To update to the latest version:

```bash
phixgo --update
```

Or if running from source:
```bash
go run . --update
```

The updater will:
1. Check for the latest release on GitHub
2. Download the appropriate binary for your OS and architecture
3. Replace the current executable with the new version
4. Keep a backup (.old) in case of issues

## Publishing Releases

To build and publish a new release:

1. Update the `version` constant in `main.go` (e.g., `v1.0.1`)
2. Run the build script:
   ```powershell
   .\build-release.ps1 -Version v1.0.1
   ```
3. Go to [GitHub Releases](https://github.com/bencewokk/phixgo/releases/new)
4. Create a new release with tag matching the version
5. Upload all zip files from the `build` directory
6. Publish the release

The build script automatically creates binaries for:
- Windows (amd64, arm64)
- Linux (amd64, arm64)
- macOS (amd64, arm64)
