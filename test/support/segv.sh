#!/usr/bin/env bash

# This is a program that will kill itself with a segmentation fault.
# It is used to simulate a crash in the test runner (e.g. rspec).
kill -SEGV $$
