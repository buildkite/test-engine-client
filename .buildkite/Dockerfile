FROM ruby:3.4.2-slim-bookworm AS ruby
FROM cypress/included:14.2.0 AS cypress
FROM python:3.13.2-bookworm AS python

FROM public.ecr.aws/docker/library/golang:1.24.1 AS golang

COPY --from=ruby / /
COPY --from=cypress / /
COPY --from=python / /

RUN gem install rspec
RUN yarn global add jest
RUN pip install pytest
