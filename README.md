# enuan-soap-gen

Build-time generator that turns a WSDL into a REST/JSON gateway in Go.

Given a WSDL, it generates a typed SOAP client (via
[`github.com/enuan/soap-go`](https://github.com/enuan/soap-go), the enuan hard
fork of `way-platform/soap-go`) and a [Huma](https://github.com/danielgtaylor/huma)-based
REST layer wired with the enuan building blocks (`web-go`, `gonfoo`,
`m3-go/log`), including OpenAPI, configuration scaffolding and tests.

- Architecture, scope and roadmap: [PLAN.md](./PLAN.md)
- Status: **F0 completed** — `github.com/enuan/soap-go` `v0.29.2-enuan.1`
  (inheritance merge, simple-type recursion guard, binding dedup, single
  module). Next: F1 (`internal/diagnose` + corpus).
