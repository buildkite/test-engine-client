"""One-shot v3.6.0 handoff. No defaults, producer, Files job, or general publish API."""

from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import HTTPRedirectHandler, Request, build_opener

BRANCH = 'te-8018-v360-publisher'
TAG = 'v3.6.0'
SOURCE = 'cf34a8992ad430601ffbde7081258ffbc3906d5f'
STAGE_SHA = 'e758f9e8d17b175a744c315c3f9daf281719f8f312e6baa84da51d5ee2feb0c8'
MANIFEST_SHA = '05d2a81b853a3be630f830fdd96f2dff517850a7bd776b643b6331a2d97f0f58'
STAGE = ('01a1f67c-d02c-48a0-9e8e-48eba8b5c32b', '01a1f680-9390-4e4c-9404-40bca7630349')
VERIFIER = ('01a1f686-a8ae-4bc9-9717-6fd441ffe92c', '01a1f687-78a6-4877-acab-3bd119678f3c')
IMAGES = ('01a1f6c6-350b-4d75-9320-506ded392fe3', '01a1f6c6-3ec1-4728-baf5-0e19b827c255')
REPOS = {'dockerhub': 'docker.io/buildkite/test-engine-client',
         'ecr': 'public.ecr.aws/buildkite/test-engine-client',
         'packages': 'packages.buildkite.com/buildkite/test-engine-client-docker/test-engine-client'}
ARCHES = ('amd64', 'arm64')
IMAGE_PINS = {
    'amd64': ('eadf69817e17e44fa4605024ec8acd47edb2a80c6c18167fdf33398e7de992dd',
              'sha256:a0039e12b2017810f5c626f8be3f5b64f21b9a39d9e8a3e7b5930bc743dd91b2'),
    'arm64': ('894ea4039076d9df9f0ee86ee8ee3ccc662dcedcc564022755d9d4c9a2ed6408',
              'sha256:cc4846eba05aa993bce0cfb354e3d12fd74eef9cb92fce7fecf3ddf3cce2ac06'),
}
INDEX = 'application/vnd.oci.image.index.v1+json'
MANIFEST = 'application/vnd.oci.image.manifest.v1+json'
DIGEST = r'sha256:[0-9a-f]{64}'


def require(test, message):
    if not test:
        raise RuntimeError(message)


def run(*argv, **kwargs):
    return subprocess.run(argv, check=True, **kwargs)


def output(*argv, **kwargs):
    return run(*argv, stdout=subprocess.PIPE, **kwargs).stdout.strip()


def hash_file(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def digest(data):
    return 'sha256:' + hashlib.sha256(data).hexdigest()


def inputs():
    env = os.environ
    require((env.get('BUILDKITE_ORGANIZATION_SLUG'), env.get('BUILDKITE_PIPELINE_SLUG'),
             env.get('BUILDKITE_BRANCH')) == ('buildkite', 'test-engine-client-release', BRANCH),
            'not the dedicated private publisher branch')
    sha = env.get('V360_PUBLISH_COMMIT', '')
    require(re.fullmatch('[0-9a-f]{40}', sha) and env.get('BUILDKITE_COMMIT') == sha
            and output('git', 'rev-parse', 'HEAD').decode() == sha, 'exact approved publisher SHA required')
    phase, target = env.get('V360_PUBLISH_PHASE'), env.get('V360_PUBLISH_TARGET')
    approval = env.get('V360_PUBLISH_APPROVAL')
    require(approval == f'{TAG}/{phase}/{target}', 'explicit phase/target approval required')
    previous = env.get('V360_LATEST_EXPECTED', '')
    if phase in ('oci-versioned', 'oci-latest'):
        require(target in REPOS, 'unknown OCI destination')
        require(bool(re.fullmatch(DIGEST, previous)) if phase == 'oci-latest' else not previous,
                'latest requires an explicit last-good digest; other phases forbid it')
    elif phase == 'native-public':
        require(target in {f'{fmt}-{arch}' for fmt in ('deb', 'rpm') for arch in ARCHES} and not previous,
                'one native format and architecture required')
        require(env.get('V360_NATIVE_CONFIRMATION') == 'owner-confirmed-absent-and-public-exposure-approved',
                'native inventory absence and public exposure must both be approved')
    else:
        raise RuntimeError('unknown or missing phase')
    require(phase == 'native-public' or not env.get('V360_NATIVE_CONFIRMATION'), 'unexpected native input')
    return phase, target, previous


def dispatch(target):
    job = dict(key='publish-approved-v360', label='Run one approved v3.6.0 destination phase',
               agents={'queue': 'oss-deploy'}, timeout_in_minutes=20,
               concurrency=1, concurrency_group='test-engine-client-release/v3.6.0-one-writer',
               retry={'manual': {'allowed': False}},
               command='python3 .buildkite/steps/v360-publisher.py execute')
    accounts = {'dockerhub': '445615400570', 'ecr': '172840064832'}
    if target in accounts:
        job['plugins'] = [{'aws-assume-role-with-web-identity#v1.4.0': {
            'role-arn': f'arn:aws:iam::{accounts[target]}:role/pipeline-buildkite-test-engine-client-release',
            'session-tags': ['organization_slug', 'organization_id', 'pipeline_slug'],
        }}]
    run('buildkite-agent', 'pipeline', 'upload', '--no-interpolation',
        input=json.dumps({'steps': [job]}).encode())


def artifact(directory, coordinates, path):
    build, job = coordinates
    run('buildkite-agent', 'artifact', 'download', path, str(directory), '--build', build, '--step', job)
    return directory / path


def native(directory, target):
    fmt, arch = target.split('-')
    name = f'bktec_3.6.0_linux_{arch}.{fmt}'
    key = f'packages/{fmt}/{name}'
    manifest = artifact(directory, VERIFIER, 'dist/verified-manifest.json')
    stage = artifact(directory, STAGE, 'dist/cli-release.tar')
    require(hash_file(manifest) == MANIFEST_SHA and hash_file(stage) == STAGE_SHA, 'native input digest mismatch')
    data = json.loads(manifest.read_bytes())
    require(data['version'] == TAG[1:] and data['source'] == SOURCE, 'wrong stage identity')
    file = directory / name
    with tarfile.open(stage) as archive:
        member, = [m for m in archive.getmembers() if m.name.removeprefix('./') == key]
        require(member.isfile(), 'native payload must be a regular file')
        with archive.extractfile(member) as src, file.open('xb') as dst:
            shutil.copyfileobj(src, dst)
    require(hash_file(file) == data['sha256'][key], 'native payload digest mismatch')
    print(f'One native POST: {name} sha256:{hash_file(file)}; stop and read back exact bytes before any retry.', flush=True)
    run('sh', '.buildkite/steps/upload-linux-packages.sh', str(file))
    print('Native POST returned success. Confirm exact inventory/served SHA-256 and signed-index exposure before another phase.')


def encode(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def image_layout(directory, receipt, archives):
    require((receipt['source'], receipt['stage_sha256'], receipt['stage_manifest_sha256']) ==
            (SOURCE, STAGE_SHA, MANIFEST_SHA) and set(receipt['images']) == set(ARCHES), 'wrong OCI stage receipt')
    layout = directory / 'oci'
    blobs = layout / 'blobs' / 'sha256'
    blobs.mkdir(parents=True)
    aliases = []
    for arch in ARCHES:
        pin = receipt['images'][arch]
        path = archives[arch]
        require((pin['archive_sha256'], pin['manifest']) == IMAGE_PINS[arch], 'OCI receipt does not match independent #699 pins')
        require(hash_file(path) == pin['archive_sha256'], 'OCI tar digest mismatch')
        with tarfile.open(path) as tar:
            members = {m.name.removeprefix('./'): m for m in tar.getmembers()}

            def blob(desc):
                normalized = desc['digest']
                require(re.fullmatch(DIGEST, normalized), 'bad OCI descriptor digest')
                sha = normalized.split(':')[1]
                member = members[f'blobs/sha256/{sha}']
                require(member.isfile() and member.size == desc['size'], 'bad OCI descriptor size')
                with closing(tar.extractfile(member)) as stream:
                    data = stream.read()
                require(digest(data) == normalized, 'OCI blob mismatch')
                (blobs / sha).write_bytes(data)
                return json.loads(data) if desc['mediaType'].endswith('+json') else None

            with closing(tar.extractfile(members['index.json'])) as stream:
                descriptor, = json.load(stream)['manifests']
            manifest = blob(descriptor)
            if 'manifests' in manifest:
                descriptor, = manifest['manifests']
                manifest = blob(descriptor)
            require(descriptor['digest'] == pin['manifest'] and descriptor['mediaType'] == MANIFEST
                    and manifest['config']['digest'] == pin['config'], 'OCI manifest or config differs from receipt')
            config = blob(manifest['config'])
            require((config['os'], config['architecture'], config['config']['Entrypoint']) ==
                    ('linux', arch, ['/usr/local/bin/bktec']), 'unexpected OCI platform or entrypoint')
            for layer in manifest['layers']:
                blob(layer)
            aliases.append(dict(mediaType=MANIFEST, digest=descriptor['digest'], size=descriptor['size'],
                                platform={'os': 'linux', 'architecture': arch}))
    index = encode(dict(schemaVersion=2, mediaType=INDEX, manifests=aliases))
    index_desc = dict(mediaType=INDEX, digest=digest(index), size=len(index))
    (blobs / index_desc['digest'].split(':')[1]).write_bytes(index)
    descriptors = {f'{TAG}-{arch}': descriptor for arch, descriptor in zip(ARCHES, aliases)}
    descriptors[TAG] = index_desc
    (layout / 'oci-layout').write_bytes(encode({'imageLayoutVersion': '1.0.0'}))
    (layout / 'index.json').write_bytes(encode(dict(schemaVersion=2, manifests=[
        dict(desc, annotations={'org.opencontainers.image.ref.name': tag}) for tag, desc in descriptors.items()
    ])))
    return layout, {tag: desc['digest'] for tag, desc in descriptors.items()}


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def get(url, authorization=None):
    headers = {'Accept': ', '.join([INDEX, MANIFEST, 'application/vnd.docker.distribution.manifest.list.v2+json',
                                   'application/vnd.docker.distribution.manifest.v2+json'])}
    if authorization:
        headers['Authorization'] = authorization
    try:
        with build_opener(NoRedirect()).open(Request(url, headers=headers, method='GET'), timeout=30) as response:
            return response.status, response.read(2_000_000)
    except HTTPError as error:
        with error:
            return error.code, error.read(65536)


def token_at(url, authorization=None):
    status, body = get(url, authorization)
    require(status == 200, f'OCI read token exchange failed (HTTP {status})')
    return json.loads(body)['token']


def auth(target, directory):
    repo = REPOS[target]
    host, name = repo.split('/', 1)
    if target == 'dockerhub':
        prefix = '/pipelines/buildkite/test-engine-client-release/dockerhub-'
        data = json.loads(output('aws', 'ssm', 'get-parameters', '--region', 'us-east-1', '--with-decryption',
                                 '--names', prefix + 'user', prefix + 'password', '--output', 'json'))
        params = {row['Name']: row['Value'] for row in data['Parameters']}
        user, password = params[prefix + 'user'], params[prefix + 'password'].encode()
    elif target == 'ecr':
        user, password = 'AWS', output('aws', 'ecr-public', 'get-login-password', '--region', 'us-east-1')
    else:
        user, password = 'buildkite', output('buildkite-agent', 'oidc', 'request-token', '--audience',
                'https://packages.buildkite.com/buildkite/test-engine-client-docker', '--lifetime', '300')
    require(password, 'empty registry credential')
    authfile = directory / 'auth.json'
    run('skopeo', 'login', '--authfile', str(authfile), '--username', user, '--password-stdin', host,
        input=password + b'\n')
    if target == 'dockerhub':
        token = token_at('https://auth.docker.io/token?' + urlencode({
            'service': 'registry.docker.io', 'scope': f'repository:{name}:pull'}))
        host = 'registry-1.docker.io'
    elif target == 'ecr':
        token = token_at('https://public.ecr.aws/token/?service=public.ecr.aws&scope=aws')
    else:
        token = token_at(f'https://{host}/v2/auth?' + urlencode({'scope': f'repository:{name}:pull'}))
    return authfile, f'https://{host}/v2/{name}/manifests', token


def current(base, token, ref):
    status, body = get(f'{base}/{ref}', 'Bearer ' + token)
    if status == 200:
        return digest(body)
    if status == 404:
        try:
            errors = json.loads(body)['errors']
        except (ValueError, KeyError, TypeError):
            errors = []
        if isinstance(errors, list) and len(errors) == 1 and isinstance(errors[0], dict) and errors[0].get('code') == 'MANIFEST_UNKNOWN':
            return None
    raise RuntimeError(f'inconclusive OCI read for {ref}: HTTP {status}')


def oci(directory, phase, target, previous):
    require(shutil.which('skopeo'), 'digest-preserving Skopeo is required on the worker')
    receipt = json.loads(artifact(directory, IMAGES, 'dist/oci/receipt.json').read_bytes())
    archives = {arch: artifact(directory, IMAGES, f'dist/oci/linux_{arch}.tar') for arch in ARCHES}
    layout, desired = image_layout(directory, receipt, archives)
    authfile, base, token = auth(target, directory)
    require(current(base, token, 'v3.5.0') == 'sha256:6acc10130e98f3d0ad1995abef587c6773ac7bdd248268cdaaf6cb007c42e446',
            'last-good OCI control differs')
    state = {ref: current(base, token, ref) for ref in desired}
    if phase == 'oci-latest':
        require(state == desired, 'all exact versioned OCI refs must match before latest')
        state = {'latest': current(base, token, 'latest')}
        require(state['latest'] in (previous, desired[TAG]), 'latest is neither approved last-good nor desired')
        pending = [('latest', TAG)]
    else:
        require(all(state[ref] in (None, sha) for ref, sha in desired.items()), 'versioned OCI conflict')
        pending = [(ref, ref) for ref in desired]
    for ref, source in pending:
        wanted = desired[source]
        actual = current(base, token, ref)
        if actual == wanted:
            print(f'{target}:{ref} already {wanted}')
            continue
        require(actual == state[ref], f'{ref} changed since preflight; stop')
        print(f'{target}:{ref} one digest-preserving copy; stop and read back before any retry.', flush=True)
        run('skopeo', 'copy', '--all', '--preserve-digests', '--dest-authfile', str(authfile),
            f'oci:{layout}:{source}', f'docker://{REPOS[target]}:{ref}')
        require(current(base, token, ref) == wanted, f'{ref} readback does not match retained digest')
        print(f'{target}:{ref} verified {wanted}')


def main():
    require(len(sys.argv) == 2 and sys.argv[1] in ('dispatch', 'execute'), 'invalid action')
    phase, target, previous = inputs()
    if sys.argv[1] == 'dispatch':
        dispatch(target)
        return
    os.umask(0o077)
    with tempfile.TemporaryDirectory() as scratch:
        if phase == 'native-public':
            native(Path(scratch), target)
        else:
            oci(Path(scratch), phase, target, previous)


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as error:
        sys.exit(str(error))
    except Exception as error:
        sys.exit(f'Publisher stopped ({type(error).__name__}); read back exact destination before retrying.')
