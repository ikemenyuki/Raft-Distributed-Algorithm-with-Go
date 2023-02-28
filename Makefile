# make command will take additional argument string as MKARGS
# e.g., make test-race MKARGS="-timeout 180s"

# folder name of the package of interest
PKGNAME = raft
MKARGS = "-timeout 3600s"

.PHONY: build test test-race checkpoint checkpoint-race clean docs
.SILENT: build test test-race checkpoint checkpoint-race clean docs

# compile the remote library.
build:
	cd src/$(PKGNAME); go build $(PKGNAME).go

# run conformance tests.
test: build
	cd src/$(PKGNAME); go test -v $(MKARGS)

checkpoint: build
	cd src/$(PKGNAME); go test -v $(MKARGS) -run Checkpoint

test-race: build
	cd src/$(PKGNAME); go test -v -race $(MKARGS)

checkpoint-race: build
	cd src/$(PKGNAME); go test -v -race $(MKARGS) -run Checkpoint

# delete executable and docs, leaving only source
clean:
	rm -rf src/$(PKGNAME)/$(PKGNAME) src/$(PKGNAME)/$(PKGNAME)-doc.txt

# generate documentation for the package of interest
docs:
	cd src/$(PKGNAME); go doc -u -all > $(PKGNAME)-doc.txt
