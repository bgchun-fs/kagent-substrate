# Building agentgateway data plane images

The [Build agentgateway image workflow](../../.github/workflows/agentgateway-image.yaml)
builds the source repository's root Dockerfile on native AMD64 and ARM64 runners,
then publishes a multi-architecture image to
`ghcr.io/kagent-dev/substrate/agentgateway`.

Once the workflow is on Substrate's default branch, select **Actions → Build
agentgateway image → Run workflow**, or run:

```sh
gh workflow run agentgateway-image.yaml --repo kagent-dev/substrate \
  -f ref=<full-agentgateway-commit-sha>
```

`ref` also accepts a branch or tag. The workflow resolves it once so both
architectures build the same commit. The source repository defaults to
`agentgateway/agentgateway`; set `repository=owner/agentgateway` to build from a
public fork.

The image tag defaults to the first 12 characters of the resolved commit SHA,
matching Substrate's existing agentgateway image pins. Pass `-f tag=<image-tag>`
to choose another tag. Reusing a tag replaces the image it points to. The binary
version is `0.0.0-alpha.<12-character-sha>`, with the full source revision embedded.
The workflow summary includes the published image digest and source commit.

This workflow only publishes data plane images. It uses the workflow's built-in
`GITHUB_TOKEN` with package write access; no separate registry secret is needed.
The GHCR package must grant the Substrate repository Actions access if it was
originally created outside this repository's workflows.
