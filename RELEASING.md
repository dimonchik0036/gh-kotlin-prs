# Releasing

1. If the output changed since the last release, re-record the README demo with `scripts/screenshots.sh`
   and commit it.
2. On `main`, make a `Release vX.Y.Z` commit that renames `## Unreleased` in [CHANGELOG.md](CHANGELOG.md) to
   `## vX.Y.Z — YYYY-MM-DD` and adds a new empty `## Unreleased` above it. That section becomes the GitHub release's
   notes as it is (step 4), so make it final before tagging: `scripts/release-notes.sh vX.Y.Z` prints them.
3. Tag that commit with an annotated tag and push both:
   ```sh
   git tag -a vX.Y.Z -m vX.Y.Z
   git push origin main vX.Y.Z
   ```
4. The tag triggers `.github/workflows/release.yml` (`cli/gh-extension-precompile`): it builds the binaries with
   `-X main.tag=vX.Y.Z`, so `gh kotlin-prs --version` prints the tag, and creates the GitHub release as a draft with
   the binaries and their attestations. A last step sets its notes, the version's CHANGELOG.md section
   (`scripts/release-notes.sh`) with a "Full Changelog" compare link to the version below it, and publishes it in the
   same call. A tag without a section, or with an empty one, fails before anything is built or created.
   - A failed run leaves at most a draft, which can still change: fix the cause and re-run the job; it uploads the
     binaries to the existing draft again and publishes it.
   - A published release is final (with immutable releases on, its binaries and tag can't change): a broken one gets
     a new patch version, never a re-tag.
5. Once the release exists, install it from GitHub and check it. `gh extension list` says what's installed first:
   the release (`dimonchik0036/gh-kotlin-prs`) or a development install from the repository.
   ```sh
   gh extension remove kotlin-prs                    # whatever is installed
   gh extension install dimonchik0036/gh-kotlin-prs
   gh kotlin-prs --version                           # prints vX.Y.Z
   ```
   Keep it installed, unless it replaced a development install; then go back to that:
   ```sh
   gh extension remove kotlin-prs
   go build && gh extension install .
   ```

## Versions

- While on 0.x: bump 0.MINOR for features and for any change to the output or the flags, 0.PATCH for fixes.
  From 1.0 on, standard semver.
- The JSON `"version"` is independent: bump it only on a breaking JSON change, and say so in the changelog.

## Pre-releases

A tag with a `-` (`v0.2.0-rc.1`) becomes a GitHub *pre-release*: the action passes `--prerelease` to
`gh release create`. `gh extension install` and `gh extension upgrade` take the latest release, which skips
pre-releases, so testers install one explicitly: `gh extension install dimonchik0036/gh-kotlin-prs --pin v0.2.0-rc.1`.
