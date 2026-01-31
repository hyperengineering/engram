# Engram Release Checklist

This document defines the definitive release process for Engram. Follow this checklist for all releases — major, minor, or patch.

## Release Types

| Type | Version Change | When to Use |
|------|----------------|-------------|
| **Major** | `v1.x.x` → `v2.0.0` | Breaking API changes, incompatible schema migrations |
| **Minor** | `v1.1.x` → `v1.2.0` | New features, backward-compatible additions |
| **Patch** | `v1.2.0` → `v1.2.1` | Bug fixes, security patches, documentation fixes |

---

## Pre-Release Checklist

### 1. Code Quality Verification

- [ ] All changes committed and pushed to `main`
- [ ] No uncommitted changes: `git status` shows clean working tree
- [ ] Run full CI pipeline locally:
  ```bash
  make ci
  ```
  This executes: `fmt` → `vet` → `lint` → `test` → `build`

- [ ] Run integration tests (requires `OPENAI_API_KEY`):
  ```bash
  make test-integration
  ```

- [ ] Verify OpenAPI spec is valid:
  ```bash
  make lint-openapi
  ```

### 2. Documentation Review

- [ ] Update `docs/` if new features or breaking changes
- [ ] Verify `docs/getting-started.md` reflects current installation methods
- [ ] Check `docs/configuration.md` for any new config options
- [ ] For **major releases**: Review all docs for accuracy

### 3. Version Planning

- [ ] Determine version number following [Semantic Versioning](https://semver.org/)
- [ ] Review commits since last release:
  ```bash
  git log $(git describe --tags --abbrev=0)..HEAD --oneline
  ```
- [ ] Confirm version bump matches change scope:
  - Breaking changes → Major
  - New features → Minor
  - Bug fixes only → Patch

### 4. Secrets Verification

- [ ] Confirm `HOMEBREW_TAP_TOKEN` is configured in GitHub repository secrets
- [ ] Confirm `OPENAI_API_KEY` is configured for integration tests
- [ ] Verify push access to `hyperengineering/homebrew-tap` repository

---

## Release Execution

### 5. Create and Push Release Tag

```bash
# Ensure you're on main and up to date
git checkout main
git pull origin main

# Create annotated tag (replace X.Y.Z with version)
git tag -a vX.Y.Z -m "Release vX.Y.Z"

# Push the tag to trigger release workflow
git push origin vX.Y.Z
```

### 6. Monitor Release Workflow

- [ ] Navigate to [GitHub Actions](https://github.com/hyperengineering/engram/actions)
- [ ] Watch `release.yml` workflow execution
- [ ] Verify all steps complete successfully:
  - [ ] Tests pass (with race detector)
  - [ ] Build verification succeeds
  - [ ] Docker images build for amd64 and arm64
  - [ ] GoReleaser completes without errors

---

## Post-Release Verification

### 7. GitHub Release Artifacts

- [ ] Navigate to [Releases](https://github.com/hyperengineering/engram/releases)
- [ ] Verify release is created with correct version
- [ ] Confirm auto-generated changelog is accurate
- [ ] Verify binary archives are present:
  - [ ] `engram_linux_amd64.tar.gz`
  - [ ] `engram_linux_arm64.tar.gz`
  - [ ] `engram_darwin_amd64.tar.gz`
  - [ ] `engram_darwin_arm64.tar.gz`
  - [ ] `engram_windows_amd64.zip`
  - [ ] `checksums.txt`
- [ ] Verify Linux packages:
  - [ ] `.deb` package
  - [ ] `.rpm` package

### 8. Docker Image Verification

```bash
# Pull and verify the new image
docker pull ghcr.io/hyperengineering/engram:vX.Y.Z

# Test the image
docker run --rm ghcr.io/hyperengineering/engram:vX.Y.Z version

# Verify multi-arch manifest exists
docker manifest inspect ghcr.io/hyperengineering/engram:vX.Y.Z
```

Expected tags created:
- [ ] `ghcr.io/hyperengineering/engram:vX.Y.Z` (manifest)
- [ ] `ghcr.io/hyperengineering/engram:vX.Y.Z-amd64`
- [ ] `ghcr.io/hyperengineering/engram:vX.Y.Z-arm64`
- [ ] `ghcr.io/hyperengineering/engram:vX` (major version, updated)
- [ ] `ghcr.io/hyperengineering/engram:latest` (updated)

### 9. Homebrew Verification

```bash
# Update tap
brew update

# Install or upgrade
brew install hyperengineering/tap/engram
# or
brew upgrade hyperengineering/tap/engram

# Verify version
engram version
```

- [ ] Homebrew formula updated in `hyperengineering/homebrew-tap`
- [ ] Installation succeeds
- [ ] Version output matches release

### 10. Binary Download Verification

```bash
# Download and verify checksum (example for Linux amd64)
curl -LO https://github.com/hyperengineering/engram/releases/download/vX.Y.Z/engram_linux_amd64.tar.gz
curl -LO https://github.com/hyperengineering/engram/releases/download/vX.Y.Z/checksums.txt

# Verify checksum
grep engram_linux_amd64.tar.gz checksums.txt | sha256sum -c -

# Extract and test
tar -xzf engram_linux_amd64.tar.gz
./engram version
```

---

## Rollback Procedure

If critical issues are discovered post-release:

### Option A: Delete Release (within hours, no adoption yet)

```bash
# Delete the GitHub release and tag
gh release delete vX.Y.Z --cleanup-tag --yes

# Delete local tag
git tag -d vX.Y.Z

# Delete remote tag (if not already deleted)
git push origin --delete vX.Y.Z
```

**Note:** Docker images and Homebrew formula require manual cleanup:
- Docker: Images remain in GHCR; consider them deprecated
- Homebrew: Push revert commit to `hyperengineering/homebrew-tap`

### Option B: Patch Release (preferred for adopted releases)

1. Fix the issue on `main`
2. Create patch release `vX.Y.Z+1`
3. Document the issue in release notes
4. Recommend upgrade path in release notes

---

## Release Artifacts Summary

Each release produces:

| Artifact | Location | Purpose |
|----------|----------|---------|
| Binary archives (5) | GitHub Release | Direct download |
| Checksums | GitHub Release | Integrity verification |
| Docker images (3) | `ghcr.io/hyperengineering/engram` | Container deployment |
| deb package | GitHub Release | Debian/Ubuntu install |
| rpm package | GitHub Release | RHEL/Fedora install |
| Homebrew formula | `hyperengineering/homebrew-tap` | macOS install |

---

## Version Injection

Version information is injected at build time via ldflags:

```go
// cmd/engram/root.go
var (
    Version = "dev"    // Overridden to tag version
    Commit  = "none"   // Overridden to short commit hash
    Date    = "unknown" // Overridden to build timestamp
)
```

Verify with: `engram version`

---

## Emergency Contacts

- **Repository**: https://github.com/hyperengineering/engram
- **Issues**: https://github.com/hyperengineering/engram/issues
- **Container Registry**: https://ghcr.io/hyperengineering/engram

---

## Changelog

GoReleaser automatically generates changelog from git commits. To ensure quality:

- Use conventional commit messages
- Commits with these prefixes are **excluded** from changelog:
  - `docs:`
  - `test:`
  - `chore:`
  - `ci:`

---

*Last updated: 2026-01-31*
