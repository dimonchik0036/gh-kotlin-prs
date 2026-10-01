# Releasing

1. If the output changed since the last release, regenerate the README screenshots with `scripts/screenshots.sh`
   and commit them.
2. On `main`, make a `Release vX.Y.Z` commit that renames `## Unreleased` in [CHANGELOG.md](CHANGELOG.md) to
   `## vX.Y.Z — YYYY-MM-DD` and adds a new empty `## Unreleased` above it.
3. Tag that commit with an annotated tag and push both:
   ```sh
   git tag -a vX.Y.Z -m vX.Y.Z
   git push origin main vX.Y.Z
   ```
4. The tag triggers `.github/workflows/release.yml` (`cli/gh-extension-precompile`): it builds the binaries with
   `-X main.tag=vX.Y.Z`, so `gh kotlin-prs --version` prints the tag, and creates the GitHub release with generated notes.
5. Once the release exists, install it from GitHub and check it:
   ```sh
   gh extension remove kotlin-prs                    # if a development install exists
   gh extension install dimonchik0036/gh-kotlin-prs
   gh kotlin-prs --version                           # prints vX.Y.Z
   gh extension remove kotlin-prs
   go build && gh extension install .                # back to the development install
   ```

## Versions

- While on 0.x: bump 0.MINOR for features and for any change to the output or the flags, 0.PATCH for fixes.
  From 1.0 on, standard semver.
- The JSON `"version"` is independent: bump it only on a breaking JSON change, and say so in the changelog.

## Pre-releases

A tag with a `-` (`v0.2.0-rc.1`) becomes a GitHub *pre-release*: the action passes `--prerelease` to
`gh release create`. `gh extension install` and `gh extension upgrade` take the latest release, which skips
pre-releases, so testers install one explicitly: `gh extension install dimonchik0036/gh-kotlin-prs --pin v0.2.0-rc.1`.
