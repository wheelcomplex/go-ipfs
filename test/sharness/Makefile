# Run tests
#
# Copyright (c) 2014 Christian Couder
# MIT Licensed; see the LICENSE file in this repository.
#

# NOTE: run with TEST_VERBOSE=1 for verbose sharness tests.

T = $(sort $(wildcard t[0-9][0-9][0-9][0-9]-*.sh))
BINS = bin/random bin/ipfs
SHARNESS = lib/sharness/sharness.sh
IPFS_ROOT = ../..

all: clean deps $(T) aggregate

clean:
	@echo "*** $@ ***"
	-rm -rf test-results
	-rm -rf bin/ipfs

$(T):
	@echo "*** $@ ***"
	./$@

aggregate:
	@echo "*** $@ ***"
	lib/test-aggregate-results.sh

deps: $(SHARNESS) $(BINS) curl

$(SHARNESS):
	@echo "*** installing $@ ***"
	lib/install-sharness.sh

bin/%: $(IPFS_ROOT)/**/*.go
	@echo "*** installing $@ ***"
	cd .. && make $@

.PHONY: all clean $(T) aggregate

# will fail if curl is not installed.
# TODO: get rid of this and install curl with git or ipfs.
install_curl_pls = "curl is required to run tests. please install it"
curl:
	@which curl >/dev/null || (echo "$(install_curl_pls)" && false)
