DOCS_OUT ?= build/docs/go-api.txt

.PHONY: docs

# docs: write `go doc -all` for every package in this module to $(DOCS_OUT).
# Run it again after changing code or doc comments.
docs:
	mkdir -p $(dir $(DOCS_OUT))
	set -e; for p in $$(go list ./...); do go doc -all -cmd $$p; echo; done > $(DOCS_OUT)
