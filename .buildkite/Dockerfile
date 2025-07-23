FROM ruby:3.4.4-slim-bookworm AS ruby
FROM cypress/included:14.5.2 AS cypress
FROM python:3.13.5-bookworm AS python

FROM golang:1.24.5-bookworm AS golang

COPY --from=ruby / /
COPY --from=cypress / /
COPY --from=python / /

RUN gem install rspec cucumber base64
RUN gem install bigdecimal -v 3.2.0
RUN yarn global add jest
RUN pip install pytest
RUN pip install buildkite-test-collector==0.2.0
RUN curl --proto '=https' --tlsv1.2 -fsSL https://static.pantsbuild.org/setup/get-pants.sh | bash -s -- --bin-dir /usr/local/bin

# Install curl, download bktec binary, make it executable, place it, and cleanup
RUN apt-get update && \
  apt-get install -y --no-install-recommends curl && \
  echo "Downloading bktec..." && \
  curl -L -o /usr/local/bin/bktec "https://github.com/buildkite/test-engine-client/releases/download/v1.5.0-rc.1/bktec_1.5.0-rc.1_linux_amd64" && \
  echo "Setting execute permissions..." && \
  chmod +x /usr/local/bin/bktec && \
  echo "bktec installed successfully:" && \
  bktec --version
