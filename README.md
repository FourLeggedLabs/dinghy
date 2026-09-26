# dinghy (fourleggedlabs)

A maintained fork of [armory/dinghy](https://github.com/armory/dinghy), which
has been dormant since July 2025. Dinghy allows you to create and maintain
Spinnaker pipeline templates in source control.

## Why this fork

Upstream development stopped in mid-2025. This fork carries the project
forward with modern dependencies and new capabilities, while keeping the core
Dinghyfile rendering engine and webhook flow byte-compatible with upstream.

### New in this fork

**GitHub App authentication** (instead of a static PAT)

```yaml
githubApp:
  appID: 123456
  installationID: 789012
  privateKeyPath: /etc/dinghy/github-app.pem   # or privateKey: <inline PEM or base64>
```

Installation tokens are cached and auto-refreshed. Falls back to
`githubToken` when `githubApp` is not configured. The App installation needs
`Contents: read`, `Commit statuses: read & write`, and (for template pushes)
`Contents: write` permissions.

**Native OpenTelemetry tracing**

Enabled by setting `OTEL_EXPORTER_OTLP_ENDPOINT` (any standard `OTEL_*` env
vars are honored — grpc and http/protobuf exporters supported):

```yaml
OTEL_EXPORTER_OTLP_ENDPOINT: otel-collector:4317
OTEL_EXPORTER_OTLP_PROTOCOL: grpc          # or http/protobuf
OTEL_SERVICE_NAME: dinghy
```

Server spans are created per request; the `buildPipelines` span carries
`dinghy.provider/org/repo/branch` attributes and records render/upsert
errors. W3C `traceparent` from upstream (e.g. Spinnaker) is propagated.

**Rich Slack notifications for pipeline updates**

Set `SLACK_BOT_TOKEN` (bot token, `xoxb-...`) and dinghy posts Block Kit
messages on pipeline create/update/delete — header with outcome, dinghyfile
path, repository, commit author/SHA/message, and errors on failure.

Channels come from the existing notification block in your Dinghyfile or
Spinnaker application:

```json
"notifications": {
  "slack": [{ "addresses": ["#ci"], "when": ["pipeline.update"] }]
}
```

Set `SLACK_DEFAULT_CHANNEL` for a catch-all channel. Channel IDs (e.g.
`C0123ABCDEF`) work as well as `#channel` names.

**GitHub commit status checks**

Unchanged from upstream and fully compatible with GitHub App auth: dinghy
posts pending/success/failure statuses (context = your `instanceId`) on the
commits that triggered processing.

### Differences from upstream

- Module renamed to `github.com/fourleggedlabs/dinghy`
- Go 1.27, go-github v33 → v74, vet-clean, distroless multi-arch image
- Removed nothing: all upstream endpoints, providers (GitHub, GitLab, Stash,
  Bitbucket Cloud/Server), and settings remain supported

---

# Upstream documentation

Dinghy allows you to create and maintain Spinnaker pipeline templates in source
control.

Read more in our
[documentation](https://docs.armory.io/docs/armory-admin/dinghy-enable/).
Dinghy allows you to create and maintain Spinnaker pipeline templates in source
control.

Read more in our
[documentation](https://docs.armory.io/docs/armory-admin/dinghy-enable/).

### How It Works

There are two primitives:
- Stage/Task templates: These are all kept in a single GitHub repo. They are
  json files with replacable values in them.
- Pipeline definitions: These define a pipeline for an application. You can
  compose stage/task templates to make a full definition.

How it works:
- GitHub webhooks are sent off when either the templates or the definitions are
  modified.
- Templates should be versioned by hash when they are used.
- Dinghy will keep a dependency graph of downstream templates. When a
  dependency is modified, the pipeline definition will be rebuilt and re-posted
  to Spinnaker. (sound familiar? haha)

### Local Development

You will need a [golang toolchain] and [make] to work on this project.

You should complete and add the file located in `example/dinghy.yml` to `/opt/spinnaker/config/dinghy.yml` since 
this is the file that dinghy search for configuration.

#### Building & Testing

You can run the `make build` and `make test` targets to build and test the
project.  You will need Redis running (either locally or in your Spinnaker
cluster), as well as Front50 and Orca.

If you have an existing Spinnaker cluster, you can port-forward to your local
machine like so:

```shell
kubectl -n spinnaker port-forward svc/spin-redis   6379
kubectl -n spinnaker port-forward svc/spin-front50 8080
kubectl -n spinnaker port-forward svc/spin-orca    8083
kubectl -n spinnaker port-forward svc/spin-fiat    7003
kubectl -n spinnaker port-forward svc/spin-echo    8089
```



#### Sample Request

```shell
curl -X POST \
  -H "Content-Type: application/json" \
  -d "@example/github_payload.json" \
  http://localhost:8081/webhooks/git/github
```

(The github_payload.json file in the example directory is a minimal set for
testing the git webhook, as an example)

Dinghy is also embedded in the [arm cli](https://github.com/armory-io/arm) tool
for local validation of pipelines.

[golang toolchain]: https://golang.org/doc/install
[make]: https://www.gnu.org/software/make/
