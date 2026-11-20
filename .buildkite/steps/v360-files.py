"""Verify frozen #697 Files inputs, then queue the existing plugin once per file."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from urllib.error import HTTPError
from urllib.request import HTTPRedirectHandler, Request, build_opener

BUILD = '01a1f686-a8ae-4bc9-9717-6fd441ffe92c'
JOB = '01a1f687-78a6-4877-acab-3bd119678f3c'
BASE = 'https://packages.buildkite.com/buildkite/bktec/files/'
# Checksum file last: expose it only after all binaries have succeeded.
FILES = {
    'bktec-darwin-amd64-3.6.0.bin': ('01a1f68a-306e-4d0c-beb0-688d5be54a98', '7b5d9abe57e8f2190ebaed3d7b374ddb0bb89442aaf0f16eb0d131144ad63917'),
    'bktec-darwin-arm64-3.6.0.bin': ('01a1f68a-306e-4edc-a00b-4d7132910c23', 'a58d3301b7538d9d73bd78ee5a6dceff7485f3d0d4945c3ae458442bd5443d2f'),
    'bktec-linux-amd64-3.6.0.bin': ('01a1f68a-306e-4aad-a6a4-5af26af056d3', 'd630c96e7be9772a46e9731d70b269d6e3fe7f084001de47cc8e91e1dc8227e7'),
    'bktec-linux-arm64-3.6.0.bin': ('01a1f68a-306e-449e-b4cb-bb31041716a6', '8714adc3d8a2895b6cc2f6ce529ab8d2656fdb947e9bcc445b160bf4b47bbfb7'),
    'bktec-windows-amd64-3.6.0.exe': ('01a1f68a-306e-4e29-9483-478425570035', '93b9d013b192b8f11e293c676d4c7767a29accc95cb765280163ae1b338a7000'),
    'bktec-windows-arm64-3.6.0.exe': ('01a1f68a-306f-42e1-bd9d-55a5a063eb82', '5a5f19733cb60245c021a066080fd17ee9ddf8ca67a7284c329576c7bc628757'),
    'bktec-checksums-3.6.0.txt': ('01a1f68a-306d-4e7a-97c9-9c60c180eb51', 'f540f71feeb386913ca6e541471a9edfdf5e2b001e989faf872bdc02b40bc4e2'),
}


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def agent(*args, **kwargs):
    return subprocess.run(['buildkite-agent', *args], check=True, **kwargs)


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def status(name, follow=False):
    request = Request(BASE + name, headers={'Cache-Control': 'no-cache'})
    opener = build_opener() if follow else build_opener(NoRedirect())
    try:
        with opener.open(request, timeout=30) as response:
            return response.status
    except HTTPError as error:
        with error:
            return error.code


def verify_and_queue(directory):
    previous = 'verify-v360-files'
    steps = []
    require(status('bktec-checksums-3.5.0.txt', follow=True) == 200, 'last-good Files control unavailable')
    for index, (name, (artifact, expected)) in enumerate(FILES.items(), 1):
        path = f'dist/buildkite-packages/{name}'
        matches = agent('artifact', 'search', path, '--build', BUILD, '--format', r'%i %j %p\n',
                        stdout=subprocess.PIPE, text=True).stdout.splitlines()
        require([line.split() for line in matches] == [[artifact, JOB, path]], f'ambiguous or changed lookup: {path}')
        agent('artifact', 'download', path, str(directory), '--build', BUILD)
        digest = hashlib.sha256()
        with (directory / path).open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(chunk)
        require(digest.hexdigest() == expected, f'digest mismatch: {path}')
        key = f'publish-v360-file-{index}'
        steps.append(dict(key=key, label=f'Publish {name}', depends_on=previous,
                          agents={'queue': 'oss-deploy'}, timeout_in_minutes=10,
                          retry={'manual': {'allowed': False}},
                          plugins=[{'publish-to-packages#v2.2.0': {
                              'registry': 'buildkite/bktec', 'artifact_build_id': BUILD, 'artifacts': path,
                          }}]))
        previous = key
        print(f'Verified {path} {expected}', flush=True)
    for name in FILES:
        observed = status(name)
        require(observed == 404, f'Files {name}: direct GET returned HTTP {observed}, not absent; stop')
    print('All exact served URLs returned 404 before queueing. Stop and read back after any uncertain POST; do not rebuild.', flush=True)
    agent('pipeline', 'upload', '--no-interpolation', input=json.dumps({'steps': steps}), text=True)


def main():
    env = os.environ
    require((env.get('BUILDKITE_ORGANIZATION_SLUG'), env.get('BUILDKITE_PIPELINE_SLUG'), env.get('BUILDKITE_BRANCH')) ==
            ('buildkite', 'test-engine-client-release', 'te-8018-v360-files'), 'wrong Files execution context')
    sha = env.get('V360_FILES_COMMIT', '')
    require(re.fullmatch('[0-9a-f]{40}', sha) and env.get('BUILDKITE_COMMIT') == sha and
            subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip() == sha,
            'exact reviewed Files commit required')
    require(env.get('V360_FILES_APPROVAL') == 'v3.6.0/files/697', 'explicit Files approval required; do not retry')
    with tempfile.TemporaryDirectory() as scratch:
        verify_and_queue(Path(scratch))


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as error:
        sys.exit(str(error))
    except Exception as error:
        sys.exit(f'Files handoff stopped ({type(error).__name__}); do not retry without authoritative readback.')
