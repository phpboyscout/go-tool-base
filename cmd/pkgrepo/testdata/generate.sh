#!/bin/sh
# Rebuilds the fixture packages, and createrepo_c's listing of two of them
# (needs docker). The golden files change with them: go test . -update
set -eu
cd "$(dirname "$0")"
export SOURCE_DATE_EPOCH=1767225600
nfpm="go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.43.1 package"

rm -rf packages createrepo
mkdir packages createrepo

for VERSION in 1.0.0 1.1.0 1.10.0; do
	for ARCH in amd64 arm64; do
		for format in deb rpm; do
			export VERSION ARCH
			$nfpm -f nfpm.yaml -p "$format" -t packages/
		done
	done
done

$nfpm -f rich.yaml -p rpm -t createrepo/
cp packages/gtb-1.0.0-1.x86_64.rpm createrepo/

docker run --rm -v "$PWD/createrepo:/r" fedora:45 sh -ec '
	dnf -q -y install createrepo_c >/dev/null 2>&1
	cd /r
	createrepo_c -q --general-compress-type=gz .
	for f in primary filelists other; do zcat repodata/*-$f.xml.gz >$f.xml; done
	rm -r repodata gtb-1.0.0-1.x86_64.rpm
	chown -R "$(stat -c %u:%g /r)" /r
'
