---
story: "6.5"
title: "Distribution Packaging and Release Automation"
status: implemented
designedBy: Clario
designedAt: "2026-01-30"
implementedBy: Spark
implementedAt: "2026-01-30"
epic: 6
epicTitle: "Operations & Distribution"
dependencies: []
frs: []
nfrs: [NFR-OPS-1, NFR-OPS-2]
---

# Technical Design: Story 6.5 - Distribution Packaging and Release Automation

## 1. Story Summary

As a **developer or team wanting to deploy Engram**, I want multiple distribution options (binary releases, Docker images, Homebrew, system packages), so that I can install and run Engram in my preferred environment with minimal friction.

**Key Requirements:**
- GoReleaser builds binaries for 5 architectures on git tag push
- Multi-arch Docker images pushed to GHCR
- Homebrew formula auto-updated in `hyperengineering/homebrew-tap`
- System packages (deb/rpm) with systemd integration
- Version info embedded via ldflags

**Phases:**
1. GoReleaser + GitHub Releases (binary distribution)
2. Docker Image (container distribution)
3. Homebrew Tap (macOS/Linux developer convenience)
4. System Packages (production server deployment)

## 2. Gap Analysis

### Current Implementation

| Component | Status | Notes |
|-----------|--------|-------|
| `.goreleaser.yaml` | Missing | No release automation |
| `Dockerfile` | Missing | Uses Paketo buildpacks via fly.toml |
| Version injection | Partial | `Version` var exists but not wired to ldflags |
| Release workflow | Missing | CI builds but doesn't release |
| Homebrew tap | Exists | https://github.com/hyperengineering/homebrew-tap (empty) |
| System packages | Missing | No deb/rpm packaging |
| Systemd service | Missing | No service unit file |

### Required Changes

**Phase 1: GoReleaser**
1. Create `.goreleaser.yaml` with multi-arch builds
2. Update `cmd/engram/root.go` for version variables
3. Create `.github/workflows/release.yml`

**Phase 2: Docker**
4. Create `Dockerfile` (multi-stage, distroless)
5. Create `.dockerignore`
6. Add Docker config to GoReleaser

**Phase 3: Homebrew**
7. Add `brews` section to GoReleaser
8. Configure `HOMEBREW_TAP_GITHUB_TOKEN` secret

**Phase 4: System Packages**
9. Create `packaging/` directory with systemd unit, config files
10. Add `nfpms` section to GoReleaser
11. Create pre/post install scripts

## 3. Interface Contract

### Version Output Format

```
engram 1.0.0 (commit: abc1234, built: 2026-01-30T12:00:00Z)
```

### Docker Image Tags

- `ghcr.io/hyperengineering/engram:v1.0.0` — specific version
- `ghcr.io/hyperengineering/engram:latest` — latest release
- `ghcr.io/hyperengineering/engram:v1.0.0-amd64` — arch-specific

### Package Naming

- `engram_1.0.0_amd64.deb`
- `engram-1.0.0-1.x86_64.rpm`
- `engram_darwin_arm64.tar.gz`

### System Package File Layout

```
/usr/bin/engram                          # Binary
/etc/engram/engram.yaml                  # Configuration (noreplace)
/etc/engram/environment                  # Environment vars (noreplace)
/usr/lib/systemd/system/engram.service   # Systemd unit
/var/lib/engram/                         # Data directory (owned by engram user)
```

## 4. Acceptance Criteria

### Phase 1: GoReleaser + GitHub Releases

1. **Given** a git tag is pushed matching `v*` pattern
   **When** the release workflow runs
   **Then** GoReleaser builds binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`

2. **Given** GoReleaser completes successfully
   **When** artifacts are uploaded
   **Then** GitHub Release contains: binaries, checksums (`checksums.txt`), and auto-generated changelog

3. **Given** a binary is downloaded
   **When** `./engram version` is executed
   **Then** the output displays the correct version, commit SHA, and build date

4. **Given** version information is embedded
   **When** the binary is built via GoReleaser
   **Then** ldflags inject `Version`, `Commit`, and `Date` from git metadata

### Phase 2: Docker Image

5. **Given** a git tag is pushed
   **When** the Docker workflow runs
   **Then** a multi-arch image (`linux/amd64`, `linux/arm64`) is built and pushed to `ghcr.io/hyperengineering/engram`

6. **Given** a Docker image exists
   **When** `docker run ghcr.io/hyperengineering/engram:v1.0.0 version` is executed
   **Then** the correct version is displayed

7. **Given** a Docker container is running
   **When** the container is started with `-v engram_data:/data`
   **Then** SQLite database persists across container restarts

8. **Given** the Dockerfile is built
   **When** the image size is measured
   **Then** the final image is under 30MB (distroless/static base)

### Phase 3: Homebrew Tap

9. **Given** GoReleaser is configured with Homebrew
   **When** a release is published
   **Then** a formula is automatically pushed to `hyperengineering/homebrew-tap`

10. **Given** the Homebrew tap exists
    **When** `brew install hyperengineering/tap/engram` is executed
    **Then** the latest release binary is installed to `/usr/local/bin/engram`

11. **Given** Engram is installed via Homebrew
    **When** a new version is released
    **Then** `brew upgrade engram` installs the new version

### Phase 4: System Packages (deb/rpm)

12. **Given** GoReleaser is configured with nfpm
    **When** a release is published
    **Then** `.deb` and `.rpm` packages are included in the GitHub Release

13. **Given** a `.deb` package is installed
    **When** the package installs
    **Then** binary is at `/usr/bin/engram`, config at `/etc/engram/engram.yaml`, data at `/var/lib/engram/`

14. **Given** a system package is installed
    **When** `systemctl start engram` is executed
    **Then** Engram starts as a systemd service

15. **Given** the systemd service is running
    **When** `systemctl status engram` is executed
    **Then** service status shows active with proper environment variables loaded

## 5. Tasks

### Phase 1: GoReleaser + GitHub Releases

- [ ] Task 1.1: Create `.goreleaser.yaml` configuration
  - [ ] Configure project name and module path
  - [ ] Define build targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`
  - [ ] Configure ldflags for version injection (`-X main.Version={{.Version}}`, etc.)
  - [ ] Set binary name to `engram`
  - [ ] Configure archives with appropriate naming (`engram_{{.Os}}_{{.Arch}}.tar.gz`)
  - [ ] Enable checksum generation
  - [ ] Configure changelog from git commits

- [ ] Task 1.2: Update `cmd/engram/root.go` for version variables
  - [ ] Add `var Version, Commit, Date string` at package level
  - [ ] Update version command/flag to display all three values
  - [ ] Add `version` subcommand if not exists

- [ ] Task 1.3: Create GitHub Actions release workflow
  - [ ] Create `.github/workflows/release.yml`
  - [ ] Trigger on tag push matching `v*`
  - [ ] Use `goreleaser/goreleaser-action@v5`
  - [ ] Configure `GITHUB_TOKEN` for release uploads
  - [ ] Run `goreleaser release --clean`

- [ ] Task 1.4: Test release process
  - [ ] Run `goreleaser release --snapshot --clean` locally
  - [ ] Verify all artifacts generated in `dist/`
  - [ ] Test binary execution and version output

### Phase 2: Docker Image

- [ ] Task 2.1: Create multi-stage Dockerfile
  - [ ] Stage 1: Go builder with `golang:1.23-alpine`
  - [ ] Copy go.mod/go.sum, download dependencies
  - [ ] Copy source, build with CGO_ENABLED=0
  - [ ] Stage 2: Runtime with `gcr.io/distroless/static-debian12`
  - [ ] Copy binary from builder
  - [ ] Set entrypoint and default command
  - [ ] Expose port 8080
  - [ ] Add labels for OCI metadata

- [ ] Task 2.2: Create `.dockerignore`
  - [ ] Exclude `.git`, `dist/`, `data/`, `.env`
  - [ ] Exclude documentation and test files

- [ ] Task 2.3: Add Docker build to GoReleaser
  - [ ] Configure docker manifest for multi-arch
  - [ ] Build `linux/amd64` and `linux/arm64` images
  - [ ] Push to `ghcr.io/hyperengineering/engram`
  - [ ] Tag with version and `latest`

- [ ] Task 2.4: Create GitHub Actions Docker workflow
  - [ ] Create `.github/workflows/docker.yml` (or integrate with release.yml)
  - [ ] Login to GHCR with `GITHUB_TOKEN`
  - [ ] Build and push multi-arch images
  - [ ] Use Docker Buildx for cross-compilation

- [ ] Task 2.5: Test Docker image
  - [ ] Build locally: `docker build -t engram:test .`
  - [ ] Run with volume: `docker run -v engram_data:/data engram:test`
  - [ ] Verify version command works
  - [ ] Verify data persistence across restarts

### Phase 3: Homebrew Tap

- [ ] Task 3.1: Verify Homebrew tap repository
  - [ ] Confirm `hyperengineering/homebrew-tap` exists (https://github.com/hyperengineering/homebrew-tap) ✓
  - [ ] Review existing README and update if needed
  - [ ] Ensure repository permissions allow GoReleaser push

- [ ] Task 3.2: Configure GoReleaser Homebrew integration
  - [ ] Add `brews` section to `.goreleaser.yaml`
  - [ ] Configure tap repository and folder
  - [ ] Set formula name, description, homepage
  - [ ] Configure dependencies (none required)
  - [ ] Set install instructions (`bin.install "engram"`)
  - [ ] Add test block (`system "#{bin}/engram", "version"`)

- [ ] Task 3.3: Configure GitHub token for tap updates
  - [ ] Create fine-grained PAT with `homebrew-tap` repo access
  - [ ] Add as `HOMEBREW_TAP_GITHUB_TOKEN` secret in main repo
  - [ ] Reference in GoReleaser config

- [ ] Task 3.4: Test Homebrew installation
  - [ ] After release, run `brew tap hyperengineering/tap`
  - [ ] Run `brew install hyperengineering/tap/engram`
  - [ ] Verify binary works and version matches release

### Phase 4: System Packages (deb/rpm)

- [ ] Task 4.1: Create systemd service unit
  - [ ] Create `packaging/engram.service`
  - [ ] Configure User/Group as `engram`
  - [ ] Set WorkingDirectory to `/var/lib/engram`
  - [ ] Set EnvironmentFile to `/etc/engram/environment`
  - [ ] Configure restart policy (on-failure)
  - [ ] Set resource limits (MemoryMax, etc.)

- [ ] Task 4.2: Create default configuration file
  - [ ] Create `packaging/engram.yaml` with production defaults
  - [ ] Configure db_path as `/var/lib/engram/engram.db`
  - [ ] Configure log format as `json`
  - [ ] Add comments documenting all options

- [ ] Task 4.3: Create environment file template
  - [ ] Create `packaging/environment`
  - [ ] Include `ENGRAM_API_KEY` placeholder
  - [ ] Include `OPENAI_API_KEY` placeholder
  - [ ] Add instructions as comments

- [ ] Task 4.4: Configure nfpm in GoReleaser
  - [ ] Add `nfpms` section to `.goreleaser.yaml`
  - [ ] Configure package name, maintainer, description
  - [ ] Set formats: `deb`, `rpm`
  - [ ] Configure file mappings (binary, config, service)
  - [ ] Configure directories: `/var/lib/engram`
  - [ ] Add pre/post install scripts for user creation
  - [ ] Configure config file handling (noreplace)

- [ ] Task 4.5: Create pre/post install scripts
  - [ ] Create `packaging/scripts/preinstall.sh` (create user/group, directories)
  - [ ] Create `packaging/scripts/postinstall.sh` (permissions, systemd reload)

- [ ] Task 4.6: Test system packages
  - [ ] Build packages with GoReleaser snapshot
  - [ ] Test `.deb` installation on Ubuntu/Debian container
  - [ ] Test `.rpm` installation on Fedora/RHEL container
  - [ ] Verify systemd service starts and runs
  - [ ] Verify data persistence and config loading

## 6. Technical Design

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                     Release Pipeline                             │
├─────────────────────────────────────────────────────────────────┤
│  git tag v1.0.0                                                  │
│       │                                                          │
│       ▼                                                          │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                    GoReleaser                                ││
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐││
│  │  │ Binaries │ │  Docker  │ │ Homebrew │ │ System Packages  │││
│  │  │ (5 arch) │ │ (2 arch) │ │ (formula)│ │ (deb/rpm)        │││
│  │  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────────┬─────────┘││
│  └───────┼────────────┼────────────┼────────────────┼──────────┘│
│          │            │            │                │            │
│          ▼            ▼            ▼                ▼            │
│   GitHub Release    GHCR      homebrew-tap    GitHub Release    │
│   (artifacts)    (images)    (formula PR)     (deb/rpm)         │
└─────────────────────────────────────────────────────────────────┘
```

### File Structure

```
engram/
├── .goreleaser.yaml              # GoReleaser configuration
├── Dockerfile                    # Multi-stage Docker build
├── .dockerignore                 # Docker build exclusions
├── packaging/
│   ├── engram.service            # Systemd unit file
│   ├── engram.yaml               # Default configuration
│   ├── environment               # Environment variables template
│   └── scripts/
│       ├── preinstall.sh         # Package pre-install script
│       └── postinstall.sh        # Package post-install script
└── .github/
    └── workflows/
        └── release.yml           # Release automation workflow
```

### Version Injection Pattern

GoReleaser injects version info via ldflags at build time:

```yaml
# .goreleaser.yaml
builds:
  - ldflags:
      - -s -w
      - -X main.Version={{.Version}}
      - -X main.Commit={{.ShortCommit}}
      - -X main.Date={{.Date}}
```

Corresponding Go code in `cmd/engram/root.go`:

```go
var (
    Version = "dev"
    Commit  = "none"
    Date    = "unknown"
)

var versionCmd = &cobra.Command{
    Use:   "version",
    Short: "Print version information",
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Printf("engram %s (commit: %s, built: %s)\n", Version, Commit, Date)
    },
}
```

### Dockerfile Design

Multi-stage build for minimal image size:

```dockerfile
# Stage 1: Build
FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o engram ./cmd/engram

# Stage 2: Runtime
FROM gcr.io/distroless/static-debian12
COPY --from=builder /build/engram /usr/local/bin/engram
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["/usr/local/bin/engram"]
```

**Why distroless/static:**
- No shell, minimal attack surface
- ~2MB base image
- Perfect for Go static binaries
- Supports multi-arch (amd64, arm64)

**CGO_ENABLED=0 requirement:**
The project uses `modernc.org/sqlite` which is pure Go, so CGO is not required. This enables static compilation.

### GoReleaser Docker Configuration

```yaml
dockers:
  - image_templates:
      - "ghcr.io/hyperengineering/engram:{{ .Tag }}-amd64"
    use: buildx
    goarch: amd64
    build_flag_templates:
      - "--platform=linux/amd64"
    dockerfile: Dockerfile

  - image_templates:
      - "ghcr.io/hyperengineering/engram:{{ .Tag }}-arm64"
    use: buildx
    goarch: arm64
    build_flag_templates:
      - "--platform=linux/arm64"
    dockerfile: Dockerfile

docker_manifests:
  - name_template: "ghcr.io/hyperengineering/engram:{{ .Tag }}"
    image_templates:
      - "ghcr.io/hyperengineering/engram:{{ .Tag }}-amd64"
      - "ghcr.io/hyperengineering/engram:{{ .Tag }}-arm64"
  - name_template: "ghcr.io/hyperengineering/engram:latest"
    image_templates:
      - "ghcr.io/hyperengineering/engram:{{ .Tag }}-amd64"
      - "ghcr.io/hyperengineering/engram:{{ .Tag }}-arm64"
```

### Homebrew Tap Repository

**Repository:** https://github.com/hyperengineering/homebrew-tap (EXISTS)

GoReleaser will push formula updates to this repository automatically on release. Ensure the `HOMEBREW_TAP_GITHUB_TOKEN` secret has write access to this repo.

### Homebrew Formula Structure

GoReleaser auto-generates formulas like:

```ruby
class Engram < Formula
  desc "Centralized lore persistence and synchronization service"
  homepage "https://github.com/hyperengineering/engram"
  version "1.0.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/hyperengineering/engram/releases/download/v1.0.0/engram_darwin_arm64.tar.gz"
      sha256 "..."
    else
      url "https://github.com/hyperengineering/engram/releases/download/v1.0.0/engram_darwin_amd64.tar.gz"
      sha256 "..."
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/hyperengineering/engram/releases/download/v1.0.0/engram_linux_arm64.tar.gz"
      sha256 "..."
    else
      url "https://github.com/hyperengineering/engram/releases/download/v1.0.0/engram_linux_amd64.tar.gz"
      sha256 "..."
    end
  end

  def install
    bin.install "engram"
  end

  test do
    system "#{bin}/engram", "version"
  end
end
```

### nfpm Configuration

```yaml
nfpms:
  - id: packages
    package_name: engram
    vendor: Hyperengineering
    maintainer: Engram Team <engram@hyperengineering.dev>
    description: Centralized lore persistence and synchronization service
    license: MIT
    formats:
      - deb
      - rpm
    bindir: /usr/bin
    contents:
      - src: packaging/engram.yaml
        dst: /etc/engram/engram.yaml
        type: config|noreplace
      - src: packaging/environment
        dst: /etc/engram/environment
        type: config|noreplace
      - src: packaging/engram.service
        dst: /usr/lib/systemd/system/engram.service
    scripts:
      preinstall: packaging/scripts/preinstall.sh
      postinstall: packaging/scripts/postinstall.sh
    overrides:
      deb:
        dependencies:
          - systemd
      rpm:
        dependencies:
          - systemd
```

### Systemd Service Unit

```ini
[Unit]
Description=Engram Lore Persistence Service
Documentation=https://github.com/hyperengineering/engram
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=engram
Group=engram
EnvironmentFile=/etc/engram/environment
ExecStart=/usr/bin/engram --config /etc/engram/engram.yaml
WorkingDirectory=/var/lib/engram
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/engram
PrivateTmp=yes

# Resource limits
MemoryMax=512M
TasksMax=100

[Install]
WantedBy=multi-user.target
```

### Pre-install Script

```bash
#!/bin/bash
set -e

# Create engram system user if not exists
if ! getent group engram >/dev/null; then
    groupadd --system engram
fi

if ! getent passwd engram >/dev/null; then
    useradd --system --gid engram --home-dir /var/lib/engram --shell /usr/sbin/nologin engram
fi

# Create data directory
mkdir -p /var/lib/engram
chown engram:engram /var/lib/engram
chmod 750 /var/lib/engram
```

### Post-install Script

```bash
#!/bin/bash
set -e

# Set permissions on config files
chmod 640 /etc/engram/environment
chown root:engram /etc/engram/environment

# Reload systemd
systemctl daemon-reload

echo ""
echo "Engram has been installed."
echo ""
echo "Next steps:"
echo "  1. Edit /etc/engram/environment to set API keys"
echo "  2. Review /etc/engram/engram.yaml for configuration"
echo "  3. Start the service: systemctl start engram"
echo "  4. Enable on boot: systemctl enable engram"
echo ""
```

## 7. Test Seeds

1. **Binary version test**: Execute `./engram version`, verify output matches tag
2. **Docker smoke test**: Run container, curl health endpoint
3. **Homebrew install test**: Install from tap, verify binary works
4. **Package install test**: Install deb/rpm in container, start service, verify health
5. **Persistence test**: Run with volume, restart, verify data survives
6. **Multi-arch test**: Pull manifest, verify both amd64 and arm64 available

## 8. Security Considerations

1. **Binary signatures**: GoReleaser can sign releases with GPG or cosign
2. **SBOM generation**: GoReleaser supports SBOM output for supply chain security
3. **Systemd hardening**: Service unit includes `NoNewPrivileges`, `ProtectSystem`, etc.
4. **Config file permissions**: Environment file with secrets is root:engram 640

## 9. Testing Strategy

**Local testing before release:**
```bash
# Test GoReleaser
goreleaser release --snapshot --clean

# Test Docker
docker build -t engram:test .
docker run -p 8080:8080 -v engram_data:/data engram:test

# Test packages (requires Docker)
docker run -v ./dist:/dist ubuntu:22.04 dpkg -i /dist/engram_1.0.0_amd64.deb
docker run -v ./dist:/dist fedora:39 rpm -i /dist/engram-1.0.0.x86_64.rpm
```

## 10. Fly.io Compatibility

The existing `fly.toml` remains valid. Users can choose:
1. **Fly.io**: Continue using Paketo buildpacks (current)
2. **Fly.io + Docker**: Update fly.toml to use the new Dockerfile
3. **Self-hosted**: Use Docker image or system packages

To use Dockerfile with Fly.io, update `fly.toml`:
```toml
[build]
  dockerfile = "Dockerfile"
```

## 11. References

- [GoReleaser Documentation](https://goreleaser.com/intro/)
- [Docker Multi-stage Builds](https://docs.docker.com/build/building/multi-stage/)
- [Homebrew Formula Cookbook](https://docs.brew.sh/Formula-Cookbook)
- [nfpm Documentation](https://nfpm.goreleaser.com/)
- [Systemd Service Units](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
- Architecture Document: `_bmad-output/planning-artifacts/architecture.md`
- Current fly.toml: `/workspaces/engram/fly.toml`
- Current Makefile: `/workspaces/engram/Makefile`

---

## Implementation Notes (Spark)

**Implemented:** 2026-01-30

### Files Created

**Phase 1: GoReleaser + Version**
- `cmd/engram/root.go` - Added `Commit`, `Date` variables and `version` subcommand
- `.goreleaser.yaml` - Full GoReleaser configuration with:
  - 5 architecture builds (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64)
  - ldflags for version injection
  - checksums and changelog generation
- `.github/workflows/release.yml` - Release workflow triggered on `v*` tags

**Phase 2: Docker**
- `Dockerfile` - Multi-stage build with distroless base
- `.dockerignore` - Build exclusions

**Phase 3: Homebrew**
- Configured in `.goreleaser.yaml` `brews` section
- Auto-updates `hyperengineering/homebrew-tap` on release

**Phase 4: System Packages**
- `packaging/engram.service` - Systemd unit with security hardening
- `packaging/engram.yaml` - Production default configuration
- `packaging/environment` - Environment variables template
- `packaging/scripts/preinstall.sh` - Creates engram user/group and directories
- `packaging/scripts/postinstall.sh` - Sets permissions and shows next steps

### Configuration Highlights

**GoReleaser:**
- Docker manifests for multi-arch images (amd64, arm64)
- Homebrew tap integration
- nfpm packages (deb, rpm) with systemd

**Docker Image:**
- Base: `gcr.io/distroless/static-debian12` (~2MB)
- Includes CA certificates for HTTPS
- Runs as non-root user
- Volume mount at `/data`

**Systemd Service:**
- Security hardening (NoNewPrivileges, ProtectSystem, PrivateTmp)
- Resource limits (512M memory, 100 tasks)
- Automatic restart on failure

### Testing

- Version command tested: `./engram version` outputs correct format
- ldflags injection verified
- All existing tests pass

### Required Secrets (for CI)

1. `HOMEBREW_TAP_GITHUB_TOKEN` - Fine-grained PAT with write access to homebrew-tap repo

### Usage

**Create a release:**
```bash
git tag v1.0.0
git push origin v1.0.0
```

**Local snapshot test:**
```bash
goreleaser release --snapshot --clean
```

---

Built and tested. Ready for release automation.
