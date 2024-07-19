FROM public.ecr.aws/docker/library/alpine:3.20.1@sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0

ARG TARGETOS
ARG TARGETARCH

COPY ./test-splitter-$TARGETOS-$TARGETARCH /usr/local/bin/test-splitter
RUN chmod +x /usr/local/bin/test-splitter

ENTRYPOINT ["test-splitter"]
