# urlshortener

[![CI](https://github.com/SpechtLabs/urlshortener/actions/workflows/ci.yaml/badge.svg)](https://github.com/SpechtLabs/urlshortener/actions/workflows/ci.yaml)
[![Release](https://github.com/SpechtLabs/urlshortener/actions/workflows/release.yaml/badge.svg)](https://github.com/SpechtLabs/urlshortener/actions/workflows/release.yaml)
[![codecov](https://codecov.io/gh/SpechtLabs/urlshortener/graph/badge.svg)](https://codecov.io/gh/SpechtLabs/urlshortener)
[![GoDoc reference](https://img.shields.io/badge/godoc-reference-blue.svg)](https://pkg.go.dev/github.com/spechtlabs/urlshortener)

A URL shortener for Kubernetes. One binary runs the controller for the
`Redirect` and `Shortlink` resources (`urlshortener.cedi.dev/v1alpha1`), serves
the shortlink redirects and the shortlink API that
[urlshortener-ui](https://github.com/SpechtLabs/urlshortener-ui) talks to.

The image is published to `ghcr.io/spechtlabs/urlshortener`: `main` for every
commit on main, and the version and `latest` for every release.

## Development

Every tool is pinned in `.mise.toml`; `mise install` installs them.

```sh
mise run check      # what CI runs: lint, go.mod, generated files, tests
mise run generate   # regenerate deepcopy methods, CRDs and RBAC after API changes
mise run image      # build the container image for the local platform
mise tasks          # everything else
```

## Contributing / Pull Requests

Please refrain from making pull requests to this repository, as this is for my own educational purposes only

[![made-with-Go](https://img.shields.io/badge/Made%20with-Go-1f425f.svg)](http://golang.org)
[![PRs Not Welcome](https://img.shields.io/badge/PRs-not_welcome-red.svg?style=flat-square)](http://makeapullrequest.com)
