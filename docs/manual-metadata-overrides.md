# Manual Metadata Overrides

When a selection strategy is set, bktec collects git metadata from your repository and sends it with the test plan request. See [Git metadata](../README.md#git-metadata) for what's collected and how to opt out.

Use `--metadata key=value` to pass additional metadata or override auto-collected values. Auto-collected values are merged with any explicit `--metadata` flags you provide, and your explicit values always take precedence.

## Usage

Use `--selection-param key=value` to pass strategy parameters. Both flags are repeatable. Values can be large and multiline.

```sh
bktec plan --json --selection-strategy manual \
  --selection-param "selectors=$(cat tests-to-run.txt)" \
  --metadata base_branch=develop
```

`--selection-param` and `--metadata` are only supported as repeatable CLI flags.
