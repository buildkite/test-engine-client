FROM ruby:3.4.3-slim-bookworm AS ruby
FROM cypress/included:14.3.2 AS cypress
FROM python:3.13.3-bookworm AS python

FROM public.ecr.aws/docker/library/golang:1.24.1 AS golang

COPY --from=ruby / /
COPY --from=cypress / /
COPY --from=python / /

RUN gem install rspec
RUN yarn global add jest
RUN pip install pytest
RUN pip install buildkite-test-collector==0.2.0
