# Why the microsandbox version is pinned exactly

`go.mod` requires `github.com/superradcompany/microsandbox/sdk/go` at exactly
`v0.6.17`, and `internal/msbversion` asserts that pin in a test.

microsandbox is a beta dependency. At the time of writing it had shipped 31
tagged versions of the Go SDK module, and it ships breaking changes at roughly
twice a week. An ambient `go get -u`, a dependency-bot commit, or a casual
`go mod tidy` against a newer minor would silently move the substrate under
this project. The pin is therefore load-bearing, and a comment saying so is not
enough on its own — a comment does not fail a build.

`internal/msbversion/version.go` holds `Required`, and
`internal/msbversion/version_test.go` reads `go.mod`, finds the require line
for the SDK module, and fails unless the two agree. It also fails if the require
line is absent entirely, so deleting the dependency does not quietly satisfy the
check. The test has been observed failing: with `Required` temporarily set to
`v0.6.16` it reported

    version_test.go:63: go.mod pins github.com/superradcompany/microsandbox/sdk/go to v0.6.17; want v0.6.16

A check that has never been shown able to fail is not a check.

## Raising the pin

Change both `Required` and the `go.mod` require line in the same commit, and
say in the commit message what upstream changed. The SDK is the only external
dependency this project has, so the diff is always small enough to read.

## The SDK is its own module

Note that the SDK is a separate Go module from the microsandbox repository root:
`github.com/superradcompany/microsandbox/sdk/go`, not
`github.com/superradcompany/microsandbox`. Both publish tags, and their version
lists differ. The import ban in `tools/importban` covers both paths so that
reaching for either one from the wrong package is caught.

## Where the SDK may be imported

Only `internal/runtime/msb/`. See `doc/import-ban.md`.
