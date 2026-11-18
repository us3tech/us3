# US3 — zwei Werkzeuge aus einem Modul.
#
#   pmpwrk   postet und pflegt                (cmd/pumpwerk)
#   turbine  streamt und lädt herunter        (cmd/turbine)

GO      ?= go
BIN     ?= bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X us3/internal/shared/version.Version=$(VERSION)

.PHONY: all pmpwrk turbine test test-short test-linux clean synology icons release site

all: pmpwrk turbine

pmpwrk:
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN)/pmpwrk ./cmd/pumpwerk

turbine:
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN)/turbine ./cmd/turbine

# Die Synology ist linux/amd64. Ohne cgo, damit nichts mitgeschleppt wird.
synology:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN)/turbine-linux-amd64 ./cmd/turbine

# Der volle Lauf braucht 7zz, par2 und Netz; test-short überspringt das.
test:
	$(GO) test ./...

test-short:
	$(GO) test -short ./...

clean:
	rm -rf $(BIN) dist

# Die FUSE-Tests brauchen einen Linux-Kernel; auf dem Mac liefert ihn
# Docker. Der Modul-Cache des Hosts reicht hinein, ins Netz geht nichts.
test-linux:
	docker build -q -t us3-fuse-test -f build/fuse-test.Dockerfile build
	docker run --rm --device /dev/fuse --cap-add SYS_ADMIN \
		--security-opt apparmor=unconfined \
		-v $(CURDIR):/src:ro -v $$($(GO) env GOMODCACHE):/gomod:ro \
		-e GOMODCACHE=/gomod -e GOPROXY=off -e GOFLAGS=-mod=readonly \
		us3-fuse-test go test ./internal/turbine/...

# Rastert die App-Icons aus den SVGs (braucht rsvg-convert) und schreibt
# .ico/.icns daraus. Nur nötig, wenn sich ein SVG ändert; das Ergebnis ist
# eingecheckt (docs/release-design.md §4).
ICON_SIZES := 16 32 48 64 128 256 512 1024

icons:
	./build/rasterize.sh pmpwrk gfx/pmpwrk-icon.svg "$(ICON_SIZES)"
	./build/rasterize.sh turbine internal/turbine/web/assets/static/icon.svg "$(ICON_SIZES)"
	$(GO) run ./build/release icons
	cp build/desktop/pmpwrk/app-256.png cmd/pumpwerk/icons/app-256.png
	cp build/desktop/turbine/app-256.png cmd/turbine/icons/app-256.png

# Baut beide Werkzeuge für Windows (amd64, arm64) und macOS (arm64) und schreibt
# die sechs ZIPs und release.json nach dist/ (docs/release-design.md §6).
release:
	GO=$(GO) $(GO) run ./build/release -version $(VERSION) -out dist

# Die Website; die Spec-Stände speist Hugo über die Umgebung ein.
site:
	HUGO_PARAMS_SPECCOMMIT_EN=$$(git log -1 --format=%h -- spec/US3-SPEC.en.md) \
	HUGO_PARAMS_SPECCOMMIT_DE=$$(git log -1 --format=%h -- spec/US3-SPEC.md) \
	hugo --source site --minify
