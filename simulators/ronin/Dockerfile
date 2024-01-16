# Build the simulator.
FROM golang:1-alpine AS builder
RUN apk --no-cache add gcc musl-dev linux-headers nodejs npm yarn

COPY --from=ghcr.io/foundry-rs/foundry /usr/local/bin/forge /usr/local/bin/forge
COPY --from=ghcr.io/foundry-rs/foundry /usr/local/bin/cast /usr/local/bin/cast
COPY --from=ghcr.io/foundry-rs/foundry /usr/local/bin/anvil /usr/local/bin/anvil
COPY --from=ghcr.io/foundry-rs/foundry /usr/local/bin/chisel /usr/local/bin/chisel

ADD . /ronin
WORKDIR /ronin
RUN go build -o sim
ENTRYPOINT ["./sim"]
#
# Build the runner container.
#FROM ghcr.io/foundry-rs/foundry
#RUN apk --no-cache add gcc musl-dev linux-headers nodejs npm yarn
#
#RUN mkdir -p /ronin
#
## COPY --from=build-environment /opt/foundry/out/forge /usr/local/bin/forge
#  #COPY --from=build-environment /opt/foundry/out/cast /usr/local/bin/cast
#  #COPY --from=build-environment /opt/foundry/out/anvil /usr/local/bin/anvil
#  #COPY --from=build-environment /opt/foundry/out/chisel /usr/local/bin/chisel
#
#
#RUN ls
#
#WORKDIR /ronin
#ENTRYPOINT ["./sim"]
