# Releasing

Releases are normally cut automatically: when [lab](https://github.com/ethpandaops/lab) publishes a stable release, it sends a `frontend-release` dispatch to this repo. The release workflow then bumps the patch version, tags it, and runs goreleaser with that exact frontend release pinned.

Manual options:

- **Run the release workflow** (`gh workflow run release.yaml`): cuts the next patch release. Leave `frontend_tag` empty to embed the latest stable lab release, or set it to any lab release tag — including alpha tags like `fusaka-v0.0.3`, which produce a suffixed backend tag (`v1.3.45-fusaka`) published as a prerelease and tagged `fusaka-latest` on Docker Hub.
- **Push a tag** (`git tag v1.3.45 && git push origin v1.3.45`): the old flow still works and embeds the latest stable lab release.

Every release records the embedded frontend version in its release notes, in the `io.ethpandaops.frontend.version` Docker label, and in the binary itself (`frontend_version` in the version endpoint output).

Deploying the image is a separate manual bump of `image.tag` in the [platform](https://github.com/ethpandaops/platform) lab application values.
