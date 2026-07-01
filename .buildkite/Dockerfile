FROM ruby:4.0.5-slim-bookworm AS ruby
FROM cypress/included:14.5.3 AS cypress
FROM python:3.15.0b3-bookworm AS python

FROM golang:1.26.4-bookworm AS golang

COPY --from=ruby / /
COPY --from=cypress / /
COPY --from=python / /

RUN git clone --depth 1 https://github.com/bats-core/bats-core.git /tmp/bats-core \
  && /tmp/bats-core/install.sh /usr/local \
  && rm -rf /tmp/bats-core
RUN gem install rspec cucumber base64
RUN gem install bigdecimal -v 3.2.0
RUN yarn global add jest
RUN pip install pytest
RUN pip install buildkite-test-collector>=1.3.0
RUN curl --proto '=https' --tlsv1.2 -fsSL https://static.pantsbuild.org/setup/get-pants.sh | bash -s -- --bin-dir /usr/local/bin
