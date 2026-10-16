# WSDL corpus (public, anonymized)

Three WSDLs used as the regression corpus for the generator and for the
diagnose stage. They originate from partner services but are **anonymized
copies**: service/port/binding/portType/operation/message names, namespaces
and endpoints are replaced by deterministic placeholders (`Service1`,
`Operation3`, `urn:example:ns1`, `https://example.invalid/service`), and
`<documentation>` elements are dropped.

XSD type and field names are preserved (unless they match a deny word) so that
assertions in generated code stay meaningful, e.g. `SessionId` and
`Successo/CodiceErrore` in `tagliandi`, `state` in `rtws`.

| File | Properties |
| ---- | ---------- |
| `chat_service.wsdl` | WCF classic; Microsoft serialization schema (self-referencing `duration` simple type); **two** SOAP 1.1 bindings on the same portType; all fields `nillable`; strings only |
| `rtws.wsdl` | ASMX; 43 operations, 180 complex types, 46 enum values, **42** `complexContent/extension` hierarchies (inherited `state`), SOAP 1.1 + 1.2 bindings |
| `tagliandi.wsdl` | ASMX; 5 operations, 7 `complexContent/extension` hierarchies (inherited `SessionId`), `Successo/CodiceErrore/LoginState` application state fields |

The original partner WSDLs are never committed to this repository.

## Regenerating the corpus

Anonymization is deterministic: running the command again on the same input
produces the same output.

```bash
make corpus-regen \
  WSDL_CHAT=/path/to/ChatService.wsdl \
  WSDL_RTWS=/path/to/rtservice.wsdl \
  WSDL_TAGLIANDI=/path/to/maintenancews.wsdl
```

That runs `enuan-soap-gen anonymize` with the deny-word lists for each source
and refreshes the diagnose golden reports (`make golden`).

## Leak policy

`enuan-soap-gen anonymize` refuses to write an output that still contains:

- any of the configured deny words (case-insensitive), in attributes or text;
- an `http(s)://` URL whose host is not in the standard-namespaces allowlist
  or the `example.invalid` placeholder.

`internal/anonymize` tests additionally assert that the committed corpus
contains none of `biztravel`, `alterdev`, `uvet`, `infocar`, `tagliandi`,
`maintenance`, `rtwebservices`.

Structural safety is checked by tests: the anonymized WSDL must have identical
`diagnose` statistics to the original (counts are rename-independent), and the
full corpus must generate code with `soap gen` (enuan fork) that builds.
