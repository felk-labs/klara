FROM --platform=$BUILDPLATFORM golang:1-trixie AS builder

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum /tmp/buildroot/
COPY cmd/imapd/ /tmp/buildroot/cmd/imapd/
COPY internal/ /tmp/buildroot/internal/
COPY pb/ /tmp/buildroot/pb/
COPY valid/ /tmp/buildroot/valid/
COPY vendor/ /tmp/buildroot/vendor/

RUN \
	cd /tmp/buildroot \
	&& \
	CGO_ENABLED=0 \
	GOARCH=${TARGETARCH} \
	GOOS=${TARGETOS} \
	go build \
		-o /opt/package/bin/imapd \
		./cmd/imapd/


FROM debian:trixie-slim

RUN \
	apt-get update \
	&& \
	apt-get install -y ca-certificates \
	&& \
	apt-get clean \
	&& \
	rm -r /var/lib/apt/lists/*

COPY --from=builder /opt/package /opt/package

WORKDIR /opt/package
ENTRYPOINT [ "bin/imapd" ]
EXPOSE 1143
