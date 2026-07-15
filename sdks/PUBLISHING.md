# Publishing the notifyd SDKs

None of the three SDKs have been published anywhere yet — no npm, PyPI, or Go module
proxy credentials exist for this project. This document is the one-time setup an owner
needs to complete before the first release of each. Nothing here should be attempted by
an automated agent; it requires account ownership and credentials that don't exist in
this repo or its CI.

## TypeScript — `@notifyd/sdk` on npm

1. Create (or gain access to) the `@notifyd` npm organization at
   https://www.npmjs.com/org/create. Scoped packages under an unclaimed org name are
   free for a single publisher.
2. Generate an npm access token with "Automation" type (bypasses 2FA prompts for CI):
   npm → Access Tokens → Generate New Token → Automation.
3. Add the token as a repository secret (`NPM_TOKEN`) in the notifyd GitHub repo's
   Settings → Secrets and variables → Actions.
4. Add a publish step to CI, gated on a git tag matching `typescript-v*`, e.g.:
   ```yaml
   - run: npm ci && npm run build && npm test
     working-directory: sdks/typescript
   - run: npm publish --access public
     working-directory: sdks/typescript
     env:
       NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
   ```
5. First publish: bump `version` in `sdks/typescript/package.json`, tag, push the tag,
   let CI publish. `npm publish` refuses to overwrite an existing version, so version
   bumps are the only way to ship an update.

## Python — `notifyd-sdk` on PyPI

1. Verify the name `notifyd-sdk` is still available: https://pypi.org/project/notifyd-sdk/
   (checked during this SDK's initial build — re-check before the actual first publish,
   since PyPI names can be claimed by anyone at any time).
2. Register an account at https://pypi.org/account/register/ if the owner doesn't have
   one, then create a PyPI API token scoped to this project (Account settings → API
   tokens) — scope it to the project after the first manual publish creates it, since
   project-scoped tokens can't be created before the project exists.
3. Add the token as a repository secret (`PYPI_API_TOKEN`).
4. Add a publish step to CI, gated on a git tag matching `python-v*`:
   ```yaml
   - run: pip install build twine
     working-directory: sdks/python
   - run: python -m build
     working-directory: sdks/python
   - run: twine upload dist/*
     working-directory: sdks/python
     env:
       TWINE_USERNAME: __token__
       TWINE_PASSWORD: ${{ secrets.PYPI_API_TOKEN }}
   ```
5. First publish: bump `version` in `sdks/python/pyproject.toml`, tag, push, let CI (or
   a manual `twine upload`) publish.

## Go — `github.com/mahrous-amer/notifyd/sdks/go`

No account or token needed — the Go module proxy (proxy.golang.org) picks up any
publicly tagged commit automatically. To cut a release:

1. Tag the commit with the nested-module tag format: `git tag sdks/go/v0.1.0`.
2. Push the tag: `git push origin sdks/go/v0.1.0`.
3. `go get github.com/mahrous-amer/notifyd/sdks/go@v0.1.0` works within minutes, once
   the module proxy has crawled it (or immediately with `GOPROXY=direct`).

No CI step is required for Go; tagging is publishing.

## Suggested tag convention

Since all three SDKs live in one repo but version independently, prefix tags by
language: `typescript-v0.1.0`, `python-v0.1.0`, `sdks/go/v0.1.0` (the `sdks/go/` prefix
on the Go tag is required by Go's nested-module versioning scheme, not a style choice).
