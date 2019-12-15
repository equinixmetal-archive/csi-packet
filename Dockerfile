# global ARG ARCH to set target ARCH
ARG BINARCH
ARG REPOARCH

## build the go binary
# build container runs on local arch
FROM golang:1.12.6 as build
ARG BINARCH

ARG pkgpath=/go/src/github.com/packethost/csi-packet/
ENV GO111MODULE=on
RUN mkdir -p $pkgpath
WORKDIR $pkgpath
# separate steps to avoid cache busting
COPY go.mod go.sum $pkgpath
RUN go mod download
COPY . $pkgpath
RUN make build install DESTDIR=/dist ARCH=${BINARCH}

## build iscsi
FROM ${REPOARCH}/gcc:9.2.0 as iscsi-build

RUN apt update && apt install -y libkmod-dev libsystemd-dev
RUN mkdir /src

WORKDIR /src
RUN git clone https://github.com/open-iscsi/open-isns.git
WORKDIR /src/open-isns
COPY isns-build.sh /tmp
RUN git checkout cfdbcff867ee580a71bc9c18c3a38a6057df0150 && /tmp/isns-build.sh && ./configure && make && make install install_hdrs install_lib

WORKDIR /src
RUN git clone https://github.com/open-iscsi/open-iscsi.git
WORKDIR /src/open-iscsi
# install to a fresh tree under /dist
RUN mkdir /dist && git checkout 288add22d6b61cc68ede358faeec9affb15019cd && make && make install DESTDIR=/dist

FROM ${REPOARCH}/ubuntu:18.04
ARG BINARCH

RUN apt-get update
RUN apt-get install -y wget multipath-tools open-iscsi curl jq

# now install latest open-iscsi, ensuring it is *after* the apt install is done
# we need to use the tmpdir, because some archs install in /usr/lib, and others in /usr/lib64
COPY --from=iscsi-build /dist /tmp/distiscsi
WORKDIR /tmp/distiscsi
RUN mv sbin/* /sbin
RUN if [ -d usr/lib64 ]; then mkdir -p /usr/lib64; mv usr/lib64/* /usr/lib64; fi
RUN if [ -d usr/lib ]; then mkdir -p /usr/lib; mv usr/lib/* /usr/lib; fi
WORKDIR /
RUN rm -rf /tmp/distiscsi

# we need to do use the tmpdir, because the COPY command cannot run $(..) and save the output
COPY --from=build /dist/packet-cloud-storage-interface-${BINARCH} /packet-cloud-storage-interface

ENV LD_LIBRARY_PATH=/usr/lib64:$LD_LIBRARY_PATH

ENTRYPOINT ["/packet-cloud-storage-interface"]
