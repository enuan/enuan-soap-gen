# enuan-soap-gen — Piano

Tool CLI che, dato un WSDL, genera un gateway REST→SOAP in Go nello stile dei
servizi enuan (Huma + web-go + gonfoo + m3-go/log, client SOAP dal fork
`github.com/enuan/soap-go`). Primo pilota: `uvetgbt-soap-go` (ChatService
Biztravel), poi i WSDL Infocar (RTWS, Tagliandi) e i futuri fornitori.

## 0. Stato e decisioni già verificate

Fatti acquisiti dagli spike (non da riverificare):

- `way-platform/soap-go` è attivo internamente ma **non mergia contributi
  esterni** (PR #11/#12/#13 ferme da 6 mesi, issue #14 senza risposta).
- `tnymlr/soap-go` è un hard fork con ~47 commit di fix utili, ma fermo dal
  2026-05-21. I suoi fix vanno importati una-tantum, non seguiti come upstream.
- Il codegen upstream/hard-fork **non gestisce `complexContent/extension`**
  (campi base persi: `SessionId` → auth rotta, `state`/`Successo` → errori non
  rilevati). Serve fix nostro.
- Due bug codegen su WSDL reali: stack overflow su `simpleType duration`
  (schema Microsoft serialization) e metodi duplicati con due binding SOAP 1.1
  sullo stesso portType.
- La pipeline "WSDL → WSDL normalizzato → `soap gen` → `go/ast` → Huma" è già
  stata prototipata con successo su ChatService (endpoint REST funzionante e
  `openapi.json` generato).
- Il flattening ricorsivo dell'ereditarietà XSD è già stato prototipato e
  verificato su RTWS e Tagliandi (SessionId e state ricompaiono nel marshal).
- **Con i fix di F0 nel fork, le trasformazioni WSDL (sanitize + flatten) non
  servono più nel percorso default**: il generatore gestisce direttamente
  schema Microsoft serialization/`duration`, binding SOAP 1.1 duplicati ed
  ereditarietà `complexContent/extension`. Restano come *diagnostica* e come
  escape hatch opzionale (vedi [2]).

## 1. Architettura

`enuan-soap-gen` è un **generatore build-time**, non un gateway runtime.
L'output è codice Go committato, revisionabile e rigenerabile:

```
WSDL (+ soapgen.yaml)
   │
   ├─ [1] ingest     parse + validazione + vendor
   ├─ [2] diagnose   check costrutti + verifica output (no transform)
   ├─ [3] clientgen  soap gen --client (fork enuan)            → internal/soap/<svc>/
   ├─ [4] model      estrazione modello (wsdl/xsd + go/ast)    → model interno
   ├─ [5] emit       Huma DTO+handler, config, main, Makefile  → codice generato
   └─ [6] verify     build, vet, golden, marshal assertions
```

Perché due sorgenti di verità al punto [4]:

- **WSDL** (`github.com/enuan/soap-go/wsdl` + `xsd`, pacchetti pubblici):
  azioni SOAP, elementi di input/output, enum, optionalità, endpoint, docs.
- **Client generato, letto via `go/ast`**: firme reali e nomi dei tipi Go.
  Così non dipendiamo dalle regole di naming interne di `soapgen`.

Un mismatch tra i due modelli è un errore fatale (segnala drift del codegen).

## 2. Pipeline in dettaglio

### [1] ingest
- Input: file locale o URL; il WSDL viene copiato/vendorizzato nel repo.
- Hash del WSDL registrato nell'header dei file generati (rigenerazioni
  idempotenti). *(implementato: `internal/ingest`, sha256 + download URL)*
- Rifiuto esplicito per: rpc/encoded, SOAP 1.2-only, `wsdl:import` esterni non
  risolvibili, MTOM/MIME. Messaggio con motivo, niente output parziale.
  *(implementato come finding `error` in [2])*
- Rilevamento `xs:include`/`xs:import` (supportati dopo i cherry-pick; se
  presenti senza supporto → errore chiaro).

### [2] diagnose

Dopo i fix di F0 il passo di normalizzazione **non è più necessario** per la
generazione. Questo stadio diventa di sola diagnostica:

1. rileva e riporta i costrutti storicamente problematici (schema Microsoft
   serialization, binding SOAP 1.1 multipli sullo stesso portType,
   `complexContent/extension`) — solo a scopo informativo/log;
2. verifica l'output del generatore (es. nessun metodo duplicato via AST,
   campi base presenti nelle struct derivate) prima di passare a [5];
3. se in futuro un WSDL colpisce un bug non ancora fixato, l'escape hatch
   `--normalize` riattiva le vecchie trasformazioni (rimozione schema, dedup
   binding, flatten ereditarietà) senza bloccare la generazione.

`--normalize` è off di default e va usato **solo come mitigazione temporanea**,
con un TODO che rimanda al fix nel fork.

*(implementato: `internal/diagnose` con regole severità error/warning/info,
report testuale deterministico + golden, `diagnose.Check(defs, raw)`; report
JSON per l'uso macchina)*

### [3] clientgen
- Invoca il CLI del fork: `soap gen --client`.
- Nel fork enuan il `cmd/soap` è nel modulo principale (single module), quindi
  funziona `go run github.com/enuan/soap-go/cmd/soap@<tag>` senza il problema
  `replace` dell'upstream. In sviluppo: path locale via `soap_bin`.
- Output in `internal/soap/<svc>/` con header "Code generated … DO NOT EDIT".

*(implementato: `internal/clientgen` — `soap gen --client -p <pkg> -i <wsdl> -d <dir>`;
binario esplicito via `soap_bin` oppure `go run github.com/enuan/soap-go/cmd/soap@<tag>`,
con tag esplicito → pin da build info → latest)*

### [4] model
Modello interno unico per operazione:

```go
type Operation struct {
    Name       string        // Echo
    SOAPAction string
    Input      *MessageModel // elementi, tipi Go, json tags, required
    Output     *MessageModel
    Docs       string
}
```

Estrazione da WSDL: `portType`, `message`, `binding` (azione, style),
`service/port` (endpoint), `xsd` (nomi/tipi/optionality/enum/docs).
Estrazione da AST: metodi `func (c *Client) X(ctx, *XWrapper, ...) (*XResponseWrapper, error)`.

*(implementato: `internal/model` — `Build(cfg, clientDir)` unisce i due modelli,
risolve i tipi annidati con una coda BFS (ordinati per nome), converte i nomi
JSON (`naming.json`), estrae enum/maxLength/docs/required; un campo WSDL
assente dalla struct Go o un'operazione senza metodo è errore fatale di
`codegen drift`)*

### [5] emit
File generati (tutti con `// Code generated by enuan-soap-gen; DO NOT EDIT.`):

| File | Contenuto |
|---|---|
| `internal/api/operations_gen.go` | DTO Input/Output (json tag, `required`, `maxLength` da XSD), `huma.Register` per ogni operazione selezionata, `mapSOAPError` |
| `internal/api/hooks.go` | generato **una sola volta**, mai sovrascritto: interfacce `SessionProvider`, `StateMapper` + stub |
| `internal/config/config_gen.go` | struct confoo (endpoint, credenziali, timeout, retry) |
| `cmd/<gw>/main.go` | config → logger (m3-go/log) → web-go + humagin → `StartServer` |
| `Makefile` | target `gen`, `gen-client`, `verify` |
| `internal/api/operations_gen_test.go` | marshal assertions e test di mapping |

Regole di mapping REST:

- default: `POST /{base_path}/{operation}` con body JSON = wrapper per document/literal.
- path/metodo per-operazione configurabili (`operations.http`).
- `complexType` sequence → oggetto JSON; `maxOccurs="unbounded"` → array;
  nillable/minOccurs=0 → `omitempty` + pointer; enum → string con valori XSD.
- `xs:decimal` → string (già così); `xs:dateTime` → `soap.XSDDateTime` quando
  cherry-pickato, altrimenti `time.Time` con warning.

*(implementato: `internal/emit` — template `text/template` embedded +
`go/format`; vedi l'Esito F2 per dettagli e limiti)*

### [6] verify
- `go build ./...`, `go vet`, `go test`.
- **Golden test** su WSDL normalizzato e file generati (corpus: ChatService,
  RTWS, Tagliandi).
- **Marshal assertions** (regressione dei bug noti):
  - Tagliandi: il marshal di `GetIntervals` contiene `<SessionId>`;
  - RTWS: i tipi risultato derivati contengono `State`;
  - ChatService: codegen senza stack overflow e con un solo metodo per operazione.
- Smoke test opzionale (`-tags smoke`) contro endpoint dev con credenziali env.

#### Corpus pubblico anonimizzato (policy)

Il corpus vive in `testdata/corpus/` ed è pubblico perché **anonimizzato**:
endpoint, namespace, service/port/binding/portType/operation/message sono
sostituiti da placeholder deterministici; i `<documentation>` sono rimossi; i
nomi XSD (tipi/campi) restano per mantenere significative le marshal
assertions. I WSDL originali dei partner non entrano mai nel repo.

- generazione: `make corpus-regen WSDL_CHAT=... WSDL_RTWS=... WSDL_TAGLIANDI=...`
  (`internal/anonymize`, deterministico);
- garanzie: `anonymize` rifiuta l'output se restano deny-word o host non
  placeholder; i test verificano stats identiche all'originale, assenza di
  deny-word e build del codice generato dal corpus;
- golden report in `testdata/golden/diagnose/` (`make golden`);
- le assertions sui WSDL reali (nomi non anonimizzati) restano test locali.

## 3. Configurazione `soapgen.yaml`

```yaml
service: rtws
wsdl: wsdl/rtservice.wsdl
module: github.com/enuan/infocar-rtws-go
base_path: /api
soap_go: github.com/enuan/soap-go      # fork
soap_bin: ""                           # vuoto = go run dal modulo fork

operations:
  include: [Login, GetVersion, GetRTDaTarga]
  exclude: []
  http:
    default: { method: POST, path: "/{operation}" }

auth:
  strategy: login                      # none | login | header
  login:
    operation: Login
    token_field: SessionId
    inject_into: context               # field del wrapper/context

error_map:
  state:                               # stato applicativo dentro HTTP 200
    success_field: Successo
    code_field: CodiceErrore
    message_field: Messaggio
    to_http: true

naming:
  json: camel
```

La config è l'unico posto in cui vive la semantica per-servizio. Tutto il
resto è meccanico.

## 4. Hook manuali (l'unica parte scritta a mano per servizio)

1. **SessionProvider**: login, cache token, refresh/expiry, eventuale logout;
   il generatore crea interfaccia + stub con TODO.
2. **StateMapper**: converte lo stato applicativo (es. `MethodState`,
   `Successo/CodiceErrore`) in errore Huma o in risposta normale.
3. **Rete/credential**: endpoint e credenziali via confoo.
4. **Selezione operazioni** (config), non codice.

Il generatore non inventa la semantica: la incapsula in interfacce tipizzate,
così un nuovo servizio richiede al massimo 2 file di glue.

## 5. Struttura repo del tool

```
cmd/enuan-soap-gen/main.go   CLI (cobra): diagnose, normalize, anonymize
internal/ingest/             file/URL + sha256
internal/diagnose/           check costrutti + normalize.go (escape hatch)
internal/anonymize/          copia di corpus anonimizzata (deny-word scan)
internal/xmltree/            DOM a offset di byte (preserva prefissi/format)
internal/clientgen/          wrapper del CLI fork
internal/config/             caricamento/validazione soapgen.yaml
internal/model/              estrazione wsdl/xsd + go/ast
internal/emit/               template (Huma, config, main, Makefile) + go/format
internal/verify/             (F3) build/golden/marshal assertions
testdata/corpus/             corpus WSDL anonimizzato + README policy
testdata/clients/            client soap-go generati (fixture dei test model/emit)
testdata/golden/diagnose/    report attesi
testdata/golden/emit/        output atteso del generatore Huma
internal/*/testdata/         fixture sintetiche (unsupported, inheritance, ...)
```

Templating: `text/template` + `go/format`/`imports` sui file emessi.
CLI: `enuan-soap-gen gen|verify|diff` (binario `enuan-soap-gen`, alias `soapgen`).

## 6. Fasi e stime

| Fase | Contenuto | Stima |
|---|---|---|
| **F0** ✅ | fork `enuan/soap-go`: cherry-pick fix tnymlr + fix nostri + rebrand modulo | fatto |
| **F1** ✅ | `internal/ingest` + `internal/diagnose` (regole + golden) + `internal/anonymize` + corpus anonimizzato + normalizer di fallback | fatto |
| **F2** ✅ | `internal/model` + `internal/emit` MVP (Huma + main/config) + `internal/config` (`soapgen.yaml`) | fatto |
| **F3** | hook auth/stato + `soapgen.yaml` + `internal/verify` con marshal assertions | 3-4 gg |
| **F4** | pilota end-to-end su `uvetgbt-soap-go` (Echo + un'operazione reale) | 2 gg |
| **F5** | opzionale: DTO JSON dedicati, docs OpenAPI arricchite, array/flatten | variabile |

Totale MVP (F0-F4): ~2,5-3 settimane. F0 è prerequisito di F1-F3 per la
verifica dei fix (i test di [2]/[6] assumono il generatore corretto).

## 7. Non-goals (scope dichiarato)

- rpc/encoded, SOAP 1.2-only, WS-Security, MTOM/MIME multipart;
- XSD `choice`/`any`/substitution group (errore esplicito, eventuale escape
  hatch `RawXML` in futuro);
- gateway runtime dinamico (WSDL caricato a runtime senza build);
- "qualsiasi WSDL": supportato il subset SOAP 1.1 document/literal ASMX/WCF
  classic (è il 100% dei WSDL in scope).

## 8. Rischi e mitigazioni

| Rischio | Mitigazione |
|---|---|
| Fork enuan divergente e trascurato | patch piccole, branch `enuan/patchset`, rebase script su release upstream, CI nel fork |
| Bug silenziosi del codegen (tipo inheritance) | marshal assertions sui 3 WSDL reali + golden test obbligatori |
| XSD→JSON non fedele (dateTime, decimal, optional) | mappature esplicite + test di contratto |
| Semantica sessione diversa per servizio | hook manuali generati, non inferenza |
| WSDL di fornitori con costrutti non supportati | rifiuto esplicito con motivo al passo [1] |
| Drift del corpus WSDL | test in CI su ogni PR |

## 9. Fork `enuan/soap-go` (F0 — completato)

Stato: **completato**, tag `v0.29.2-enuan.1` (merge su `main`, branch
`enuan/patchset` conservato per la gestione patch).

- fork creato: `github.com/enuan/soap-go` (da `way-platform/soap-go`);
- clone locale: `~/m3/projects/go/src/github.com/enuan/soap-go`;
- remote: `origin` (enuan), `upstream` (way-platform), `tnymlr`;
- branch di lavoro `enuan/patchset` (pushato), base `942cd2a` = upstream/main = v0.29.2.

Cherry-pick da `tnymlr/main` (47 commit; `-x` per provenienza, saltando rebrand
e merge commit):

| Tema | Commit |
|---|---|
| enum: sanitizzazione + ordinamento stabile | `0766525`, `eaa7cf6` |
| collisioni elemento/tipo | `fd04808`, `58c6637`, `f255d20`, `de44d3a`, `d67ab9c` |
| namespace: prefix naming + fix | `5ff67a3`, `066cdf7`, `25f4b63` |
| `xs:include` / `xs:import` | `6669ab2`, `cb0c303`, `55adb4e`, `7b68baa` |
| simpleType non-enum | `b5610e9` |
| `soap:header` codegen | `e7a4b27` |
| `xs:all` + rawXML heuristic | `96e53ee`, `04c56e8` |
| `dateTime` senza timezone (`soap.XSDDateTime`) | `3f8e439` |
| booleani wire `0`/`1` | `cf26334` |
| single-module packaging (no nested `cmd/soap` con replace) | `b8195ad` (+ README/install `8324da4`, `b4dee70`) |

Da NON importare: rebrand tnymlr (`9580e35`, `07952771`), bump pseudo-version
del nested module, "Use tnymlr repo", merge commit.

Fix nostri da aggiungere:

1. `complexContent/extension`: merge dei campi base (ricorsivo, cycle-safe),
   sia per complexType nominati che inline; golden su Tagliandi (`SessionId`)
   e RTWS (`state`).
2. Ricorsione infinita su simpleType locale che shadowa un primitivo XSD
   (`duration` vs `xs:duration`): guardia con set di tipi visitati/qualifica.
3. Dedup metodi client con due binding SOAP 1.1 sullo stesso portType.

Ordine di lavoro consigliato (minimizza i conflitti):

1. cherry-pick dei fix verdi su base upstream (modulo ancora `way-platform`);
2. fix nostri;
3. rebrand a `module github.com/enuan/soap-go` (commit meccanico finale);
4. tag `v0.29.2-enuan.1`, merge `enuan/patchset` → `main`, push;
5. apertura PR upstream (facoltativa, costo basso, aspettativa zero).

### Esito F0

- Cherry-pick: 21 commit da `tnymlr/main` + 2 commit di fixup import (tutti con
  `-x`, provenienza in history).
- 3 fix enuan, uno per commit, ciascuno con golden di regressione:
  - `377d756` inheritance merge → `testdata/complex_content_extension`;
  - `a81d642` simpleType auto-referenziale → `testdata/self_referential_simple_type`;
  - `089c42f` dedup binding SOAP 1.1 → `testdata/duplicate_soap11_bindings`.
- Rebrand `github.com/enuan/soap-go` (commit `a4d0c64`), esempi rigenerati
  (`aa38c5f`), tag annotato `v0.29.2-enuan.1`, merge `fce7091` su `main`.
- Verifica su WSDL reali con il CLI del fork: ChatService **raw** (senza
  sanitize) genera senza stack overflow e con 1 solo metodo `Echo`; Tagliandi
  ha `BaseContext.SessionId`; RTWS ha `RTDaTargaResult.State`; i tre package
  generati compilano e passano `go vet`.
- Distribuzione verificata: `go list -m` risolve via proxy pubblico e
  `go install github.com/enuan/soap-go/cmd/soap@v0.29.2-enuan.1` produce
  `soap version v0.29.2-enuan.1` (il bug del nested module è risolto).
- Conseguenza per il piano: lo stadio [2] resta `diagnose` (non `normalize`);
  `sanitize`/`flatten` non sono più necessari sul percorso default.

### Esito F1 (completato)

- `cmd/enuan-soap-gen` (cobra): `diagnose` (`--json`, `--fail-on`),
  `normalize` (escape hatch, tutte le trasformazioni on di default), `anonymize`
  (`--deny`, leak scan obbligatorio).
- `internal/ingest`: file locale o URL (download + sha256, limite 32 MiB).
- `internal/diagnose`: regole severity error/warning/info — unsupported
  (rpc/encoded/mime/soap12-only/xsd-choice/xsd-any/substitution-group/
  abstract/wsdl-import), known-pitfall (schema Microsoft serialization,
  simpleType che shadowa un primitivo, binding SOAP 1.1 duplicati),
  config-hint (session/state field), warning (untyped element, one-way,
  list/union, group ref, mixed, base non risolto) + stats rename-independent.
  Report testuale deterministico + JSON.
- `internal/diagnose/normalize.go`: fallback token/byte-range — rimozione
  schema MS serialization, dedup binding SOAP 1.1 (con relativi port), flatten
  ricorsivo `complexContent/extension` (attributi inclusi, cycle-safe,
  choice/group lasciati intatti e segnalati); idempotente.
- `internal/xmltree`: DOM a offset di byte per trasformazioni che preservano
  prefissi/format (il round-trip con `xml.Marshal` sulle struct wsdl è lossy).
- `internal/anonymize`: rinomina placeholder deterministica di
  service/port/binding/portType/operation/message, namespace → `urn:example:nsN`,
  endpoint → `https://example.invalid/service`, drop `<documentation>`,
  rename dei simboli XSD solo se matching deny-word; leak scan (deny-word +
  host non placeholder) e rifiuto dell'output.
- Corpus pubblico `testdata/corpus/` (chat_service, rtws, tagliandi): stats
  identiche agli originali, nessuna deny-word, golden report in
  `testdata/golden/diagnose/`; il corpus genera codice col fork rilasciato che
  compila e passa `go vet`.
- Test: golden + asserzioni sintetiche (rpc/encoded, soap12-only, import,
  choice/any/substitutionGroup, http binding), normalizer (idempotenza,
  campi ereditati `SessionId`/`state`), anonymizer (leak scan, stats),
  xmltree, ingest. `make ci` locale + GitHub Actions.

### Esito F2 (completato)

Pipeline completa `gen` (CLI: `enuan-soap-gen gen --config soapgen.yaml`):

1. **ingest + diagnose** (bloccante sugli `error`); 2. **clientgen**; 3. **model**;
4. **emit**; 5. `go mod tidy` (disattivabile con `--no-tidy`).
Flag: `--client-only` (solo client), `--skip-client` (riusa il client esistente).

**`internal/config`** (atterrato in F2, previsto in F3): `soapgen.yaml` con
`KnownFields(true)`, default per base path (`/api`), timeout 30s, retries 1
(operazioni SOAP non idempotenti), dir di output (`internal/soap/<pkg>`,
`internal/api`, `cmd/<gateway>`), regole HTTP (`operations.http`, default
`POST /{operation}` con `{operation}` → camelCase), naming JSON (`camel` o
`preserve`), `auth` e `error_map` parse-validati ma **non ancora usati dai
template** (F3).

**`internal/model`**: estrazione da WSDL (portType/binding/endpoint/xsd) + AST
del client generato; merge con errore fatale su drift; tipi annidati via BFS
(ordinati), enum, `maxLength`, `required` (minOccurs/nillable), docs, naming
JSON. Test su fixture commitate (niente CLI nei test unitari).

**`internal/emit`**: 7 file — `operations_gen.go` (DTO piatte + entità annidate
`<Tipo>DTO`, converter bidirezionali `xFromSOAP`/`xToSOAP`, un `huma.Register`
per operazione, `mapSOAPError`: deadline→504, `soap.Fault`→502),
`hooks.go` (**una sola volta**, mai sovrascritto: `SessionProvider`,
`StateMapper`, `Hooks`), `operations_gen_test.go` (marshal assertion sui campi
del wrapper + handler con `httptest` che verifica SOAPAction e round-trip),
`config_gen.go` (confoo, endpoint dal WSDL + override), `main.go`
(web-go + humagin + `StartServer`, `m3-go/log`, endpoint esplicito e
`WithMaxRetries`), `Makefile`, `go.mod` con i pin runtime documentati.

Bug trovati e corretti durante la validazione:

- le DTO dei tipi annidati erano referenziate dai converter ma **non
  dichiarate** (il progetto generato non compilava): ora la dichiarazione è
  emessa per ogni tipo del modello;
- nei converter `ToSOAP` degli array l'elemento era `[]<Tipo>DTO` invece di
  `[]<tipo client>`.

Verifiche:

- chat_service: golden `testdata/golden/emit/chat_service.operations_gen.go`,
  determinismo su due run, `hooks.go` preservato;
- tagliandi: asserzione AST che ogni `<Tipo>DTO` usato sia dichiarato,
  `BaseContextDTO.SessionId` presente (regressione ereditarietà);
- rtws (stress, 43 op/180 complexType): `operations_gen.go` ~9.700 righe,
  generazione ~2s, `go build`/`go vet`/`go test` puliti;
- pilota reale ChatService: compila e i test generati passano;
- fixture `testdata/clients/` confrontate con il CLI `v0.29.2-enuan.1`
  (`SOAPGEN_E2E=1`), rigenerabili con `make testdata-regen`;
- e2e: `make e2e` genera un progetto completo e vi esegue
  `go mod tidy` + `go build` + `go vet` + `go test`; attivo in CI.

Prossimo (F3): `SessionProvider`/`StateMapper` collegati da `auth`/`error_map`,
`internal/verify` (marshal assertions sui tipi risultato: Tagliandi
`<SessionId>`, RTWS `State`, ChatService metodo singolo), poi F4 pilota
`uvetgbt-soap-go`.
