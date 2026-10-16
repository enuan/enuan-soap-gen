# enuan-soap-gen

Build-time generator that turns a WSDL into a REST/JSON gateway in Go.

Given a WSDL, it generates a typed SOAP client (via
[`github.com/enuan/soap-go`](https://github.com/enuan/soap-go), the enuan hard
fork of `way-platform/soap-go`) and a [Huma](https://github.com/danielgtaylor/huma)-based
REST layer wired with the enuan building blocks (`web-go`, `gonfoo`,
`m3-go/log`), including OpenAPI, configuration scaffolding and tests.

- Architecture, scope and roadmap: [PLAN.md](./PLAN.md)
- Status: **F4 completed** — full `gen` pipeline (ingest → diagnose → client →
  model → Huma emit → verify) with session auth, application-state mapping,
  generated REST/marshal assertions, structural verification and a real pilot
  (`uvetgbt-soap-go`: ChatService gateway, smoke-tested against a fake SOAP
  backend). Next (optional): upstream PRs, richer OpenAPI docs, new services.

## Install

```sh
go install github.com/enuan/enuan-soap-gen/cmd/enuan-soap-gen@latest
```

## Usage

```sh
# Diagnose a WSDL (no output written).
enuan-soap-gen diagnose -i wsdl/chat.wsdl --fail-on error

# Generate client + gateway in the current module.
cat > soapgen.yaml <<'YAML'
service: chat
wsdl: wsdl/chat.wsdl
module: github.com/enuan/chat-gateway
YAML
enuan-soap-gen gen --config soapgen.yaml
```

`gen` runs: ingest → diagnose (fails on `error` findings) → `soap gen
--client` → model extraction → emission → `go mod tidy` → verify. Flags:
`--client-only`, `--skip-client`, `--no-tidy`, `--run-tests`.

Standalone verification of an already generated project:

```sh
enuan-soap-gen verify --config soapgen.yaml            # structural checks
enuan-soap-gen verify --config soapgen.yaml --run-tests # + generated tests
enuan-soap-gen verify --config soapgen.yaml --json      # machine readable
```

### Generated project

| Path | Content |
|---|---|
| `internal/soap/<service>/` | soap-go client (types + methods) |
| `internal/api/operations_gen.go` | DTOs, bidirectional converters, `huma.Register` per operation, `mapSOAPError` |
| `internal/api/hooks.go` | written **once**, never overwritten: `SessionProvider`, `StateMapper` |
| `internal/api/auth_gen.go` | lazy login with cached token (`auth.strategy=login`) |
| `internal/api/operations_gen_test.go` | marshal assertions, handler round-trip, REST request per operation (`humagin` + JSON), OpenAPI document, login, token injection and state-error tests |
| `internal/config/config_gen.go` | confoo configuration (WSDL endpoint as default) |
| `cmd/<gateway>/main.go` | web-go + humagin server, m3-go/log |
| `Makefile`, `go.mod` | pinned runtime stack, `gen`/`verify` targets |

Every operation becomes `POST /api/<camelCaseOperation>` (configurable through
`operations.http`); SOAP faults map to `502`, timeouts to `504`.

### `soapgen.yaml`

```yaml
service: rtws
wsdl: wsdl/rtservice.wsdl
module: github.com/enuan/infocar-rtws-go
base_path: /api
soap_bin: ""              # empty: go run github.com/enuan/soap-go/cmd/soap
operations:
  include: [Login, GetVersion]     # empty = all
  http:
    default: { method: POST, path: "/{operation}" }
auth:
  strategy: login                  # none | login | header  (wired in F3)
error_map:
  state:                           # application state inside HTTP 200 (F3)
    success_field: Successo
naming:
  json: camel                      # camel | preserve
```

## Development

```sh
make ci                          # build + vet + test
make golden                      # refresh diagnose/emit golden files
make testdata-regen SOAP_BIN=... # refresh testdata/clients fixtures (needs soap CLI)
make e2e SOAP_BIN=...            # end-to-end: needs the soap CLI, the network
                                 # and read access to the private enuan modules
```

CI runs `make ci` only: the end-to-end tests generate a project that imports
the private enuan stack (`m3-go`, `gonfoo`, `web-go`), so they are local-only.

Test strategy:

- `testdata/corpus/`: anonymized public WSDLs (see its README for the policy);
- `testdata/clients/`: committed soap-go clients, so model/emit tests run
  without the CLI; `SOAPGEN_E2E=1` regenerates and diffs them;
- `testdata/golden/`: deterministic expectations for diagnose reports and the
  generated `operations_gen.go`.
