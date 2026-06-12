FROM ruby:4.0.5-slim-bookworm AS ruby
FROM cypress/included:14.5.3 AS cypress
FROM python:3.15.0b2-bookworm AS python
# Latest released bktec image. This is used to split tests on `main`. On other
# branches the test step builds bktec from source instead (see
# .buildkite/steps/tests.sh). The `latest` tag only points at stable releases.
FROM public.ecr.aws/buildkite/test-engine-client:latest AS bktec

FROM golang:1.26.4-bookworm AS golang

COPY --from=ruby / /
COPY --from=cypress / /
COPY --from=python / /
COPY --from=bktec /usr/local/bin/bktec /usr/local/bin/bktec

RUN gem install rspec cucumber base64
RUN gem install bigdecimal -v 3.2.0
RUN yarn global add jest
RUN pip install pytest
RUN pip install buildkite-test-collector>=1.3.0
RUN curl --proto '=https' --tlsv1.2 -fsSL https://static.pantsbuild.org/setup/get-pants.sh | bash -s -- --bin-dir /usr/local/bin

RUN echo "bktec installed successfully:" && bktec --version
