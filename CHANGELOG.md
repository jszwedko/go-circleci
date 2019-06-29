# Change Log

**ATTN**: This project uses [semantic versioning](http://semver.org/).

## [Unreleased]

## 0.3.0 - 2019-06-29

Added:

* `Build`s now return `Workflow` information
* `Build`s now return `Picard` information which describes the execution environment
* `Build`s now return `Platform` information
* `Action`s now return `Background` information
* `CommitDetails` now return `Branch` and `PullRequest`
* `BuildOpts` method for building a project with arbitrary parameters

Bug fixes:

* Fix issue with paginating queries returning a 401
* Actually send build parameters for `ParameterizedBuild`
* Fix feature flag parsing to not cause a null pointer panic

## 0.2.0 - 2018-03-10

Backwards incomptabile changes:

* Switched `FeatureFlags` to a `struct` from `map[string]bool` as it was found that not all feature flags are `bool`s
  (https://github.com/jszwedko/go-circleci/issues/8) which resulted in non-bool values being inaccessible. Known feature
  flags are encoded as struct fields with a `.Raw()` method to access the underlying `map[string]interface{}` to access
  unknown feature flags.

## 0.1.0 - 2018-03-10

### Added
- Initial implementation.
