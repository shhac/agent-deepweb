---
description: Build, release, and publish to Homebrew
argument-hint: <patch|minor|major>
---

# Release

Perform a full release: version bump, build, GitHub release, and Homebrew tap update.

## Arguments

- `$ARGUMENTS` — version bump type: `patch`, `minor`, or `major`

## Instructions

You are performing a release of the `agent-deepweb` CLI (Go version). Follow these steps exactly.

### Pre-flight

1. Confirm the working tree is clean (`git st`). If not, stop and ask.
2. Run `make test` and `go vet ./...`. If either fails, stop and fix.
3. Determine the current version from the latest git tag (`git describe --tags --abbrev=0`) and show what bump will happen. If no tag exists, start at `0.1.0`.

### Step 1: Version bump, tag, and push

Calculate the new version by bumping the current tag:

```bash
# Get current version
current=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo "0.0.0")
# Split into parts and bump based on type
IFS='.' read -r major minor patch <<< "$current"
```

Apply the bump type ($ARGUMENTS):
- `patch`: increment patch
- `minor`: increment minor, reset patch to 0
- `major`: increment major, reset minor and patch to 0

Then tag and push:

```bash
git tag "v${new_version}"
git push origin main "v${new_version}"
```

### Step 2: Build with goreleaser

```bash
goreleaser release --clean
```

If `goreleaser` is not installed, build manually:

```bash
rm -rf dist/
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w -X main.version=${new_version}" -o "dist/agent-deepweb-darwin-arm64" ./cmd/agent-deepweb
GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w -X main.version=${new_version}" -o "dist/agent-deepweb-darwin-amd64" ./cmd/agent-deepweb
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=${new_version}" -o "dist/agent-deepweb-linux-amd64" ./cmd/agent-deepweb
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w -X main.version=${new_version}" -o "dist/agent-deepweb-linux-arm64" ./cmd/agent-deepweb
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -X main.version=${new_version}" -o "dist/agent-deepweb-windows-amd64.exe" ./cmd/agent-deepweb

# Create tarballs
cd dist
for bin in agent-deepweb-darwin-arm64 agent-deepweb-darwin-amd64 agent-deepweb-linux-amd64 agent-deepweb-linux-arm64; do
  tar czf "${bin}.tar.gz" "$bin"
done
shasum -a 256 *.tar.gz agent-deepweb-windows-amd64.exe > checksums-sha256.txt
cd ..
```

### Step 3: Create GitHub release

If goreleaser handled it, skip this step. Otherwise:

```bash
# Generate release notes
prev_tag=$(git tag --sort=-v:refname | head -2 | tail -1)
notes=$(git log --pretty=format:"- %s" "${prev_tag}..v${new_version}" --no-merges | grep -v "^- v[0-9]")

gh release create "v${new_version}" dist/*.tar.gz dist/agent-deepweb-windows-amd64.exe dist/checksums-sha256.txt \
  --title "v${new_version}" \
  --notes "$notes"
```

Verify: `gh release view "v${new_version}"`

### Step 4: Update Homebrew tap

The Homebrew formula lives in `../homebrew-tap` relative to this repo's root.

```bash
ls ../homebrew-tap/Formula/agent-deepweb.rb
```

**If it doesn't exist:** Create it by copying the pattern from `../homebrew-tap/Formula/agent-sql.rb`, replacing:
- Class name: `AgentDeepweb`
- desc: `"Authenticated HTTP CLI for AI agents (curl-with-auth)"`
- homepage: `https://github.com/shhac/agent-deepweb`
- All `agent-sql` references → `agent-deepweb`
- Version, URLs, and SHA256 values
- Test: assert_match version and "deepweb" in help output

**If it exists:** Read checksums from `dist/checksums-sha256.txt` and update the formula:

1. Read `../homebrew-tap/Formula/agent-deepweb.rb`
2. Update version, URLs (use `v${new_version}`), SHA256 values, assert_match version
3. Note: Go binaries use `amd64` not `x64` — update the tarball names accordingly
4. Commit and push:
   ```bash
   cd ../homebrew-tap
   git add Formula/agent-deepweb.rb
   git commit -m "agent-deepweb ${new_version}"
   git push
   cd -
   ```

**IMPORTANT:** Always `cd` back to the agent-deepweb repo after updating the tap.

### Step 5: Report

Show the user:

- New version number
- GitHub release URL
- Homebrew tap commit (if applicable)
- `brew install shhac/tap/agent-deepweb` command for new users
- `brew upgrade shhac/tap/agent-deepweb` command for existing users
