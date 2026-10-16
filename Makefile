GO ?= go

.PHONY: build test vet fmt golden corpus-regen ci

build:
	$(GO) build ./...

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w cmd internal

# Regenerate the deterministic diagnose golden reports.
golden:
	$(GO) test ./internal/diagnose -run TestGoldenReports -update

# Regenerate the public WSDL corpus from the partner originals.
#
# The originals are NOT part of this repository (they are partner-confidential);
# point the variables at local copies. See testdata/corpus/README.md.
corpus-regen:
	@test -n "$(WSDL_CHAT)" || (echo "WSDL_CHAT is required" && exit 1)
	@test -n "$(WSDL_RTWS)" || (echo "WSDL_RTWS is required" && exit 1)
	@test -n "$(WSDL_TAGLIANDI)" || (echo "WSDL_TAGLIANDI is required" && exit 1)
	$(GO) run ./cmd/enuan-soap-gen anonymize -i "$(WSDL_CHAT)" -o testdata/corpus/chat_service.wsdl --deny biztravel,alterdev,alter,chat --allow-no-deny
	$(GO) run ./cmd/enuan-soap-gen anonymize -i "$(WSDL_RTWS)" -o testdata/corpus/rtws.wsdl --deny infocar,rtwebservices --allow-no-deny
	$(GO) run ./cmd/enuan-soap-gen anonymize -i "$(WSDL_TAGLIANDI)" -o testdata/corpus/tagliandi.wsdl --deny infocar,maintenance,tagliandi --allow-no-deny
	$(MAKE) golden

ci: build vet test
