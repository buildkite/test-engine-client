# Rehearse a legacy Go source update

The canonical CLI is developed in `buildkite/bktest`. Use this tooling from the
legacy repository to rehearse updating its Go source from a reviewed canonical
commit. This does not build or publish release assets.

```sh
python3 util/legacy-source/project.py generate "$canonical_repo" "$canonical_sha" "$projection"
python3 util/legacy-source/project.py check "$canonical_repo" "$canonical_sha" "$projection"
```

Use a full commit SHA and a new output directory. The manifest maps the CLI
source and its fixtures to the old module, moving `cmd/bktest` back to the root.
Review manifest changes when canonical CLI ownership changes; do not include
collector or shared repository directories. Legacy README, repository metadata,
CI and distribution configuration stay at the legacy boundary. The projection
contains `.bktest-source` with the canonical commit and preserves Git executable
bits. `check` compares exact file membership, bytes, and executable bits.

## Local preview

Use a disposable, full clone of the legacy repository with historical tags.
Remove its push remote before advancing any local refs. In that clone, start at
the reviewed legacy baseline (including the tooling if it has already landed).
Replace only the projected areas, deleting stale files before copying:

```sh
git -C "$preview" rm -r -- ':(top,glob)*.go' docs internal util/supported_features
cp -a "$projection"/. "$preview"/
find "$projection" -type f -printf '%P\0' |
  git -C "$preview" --literal-pathspecs add -f --pathspec-from-file=- --pathspec-file-nul
git -C "$preview" diff --cached --stat
```

Force-add only the projected files: some tracked test fixtures match the legacy
`.gitignore`. Review the staged diff before committing. The generated commit must
descend from the old default branch and leave other files and historical refs untouched.
Run the root build with `GOWORK=off go build -mod=readonly .`. In the disposable
legacy clone, fast-forward `main` to the generated commit and add a new rehearsal
version tag. In a separate disposable canonical clone, put the same tag on the
pinned canonical SHA. This also lets Go resolve the canonical module's `@latest`
for its deprecation check when the canonical default branch has no CLI module yet.
Use the exact package paths from a directory outside either Go module:

```sh
bash util/legacy-source/verify-install.sh legacy "$historical_repo" "$legacy_base" "$legacy_base" bktec "$old_check"
bash util/legacy-source/verify-install.sh canonical "$canonical_preview" "$rehearsal_tag" "$canonical_sha" bktest "$new_check"
bash util/legacy-source/verify-install.sh legacy "$preview" main "$preview_sha" bktest "$main_check"
bash util/legacy-source/verify-install.sh legacy "$preview" "$rehearsal_tag" "$preview_sha" bktest "$tag_check"
```

Each check takes a different new scratch directory. It resolves the actual
module path against a local Git repository, checks the resolved commit, then
checks the installed filename, help and injected version. No Go workspace,
module replacement, or published tag is needed. For a stable update, also
verify `latest` resolves to the new snapshot in the local clone.

Releases must continue to advance the legacy Go source and tag each reviewed
snapshot at the corresponding version, without rewriting history. This local
procedure does not authorize pushing snapshots, publishing tags, or running
the legacy release pipeline.
