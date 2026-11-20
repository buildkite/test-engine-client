"""One-off v3.6.0 preflight: GETs, OIDC issuance and AWS read APIs only."""

import base64
import hashlib
import json
import os
import subprocess
import sys
from urllib.error import HTTPError
from urllib.parse import parse_qs, urlencode, urljoin, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


PACKAGE_HOST = 'https://packages.buildkite.com'
PACKAGE_API = 'https://api.buildkite.com/v2/packages/organizations/buildkite/registries'
TAGS = ('v3.5.0', 'v3.6.0', 'v3.6.0-amd64', 'v3.6.0-arm64')
ACCEPT = ', '.join('application/vnd.' + media for media in (
    'oci.image.index.v1+json', 'oci.image.manifest.v1+json',
    'docker.distribution.manifest.list.v2+json', 'docker.distribution.manifest.v2+json',
))


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def get(url, authorization=None, accept='application/json', public_redirect=False):
    assert not (public_redirect and authorization)
    headers = {'Accept': accept}
    if authorization:
        headers['Authorization'] = authorization
    opener = build_opener() if public_redirect else build_opener(NoRedirect())
    try:
        with opener.open(Request(url, headers=headers, method='GET'), timeout=25) as response:
            return response.status, response.read(), response.headers
    except HTTPError as error:
        with error:
            return error.code, error.read(65536), error.headers


def command(*args, env=None):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env,
                            text=True, check=False, timeout=60)
    return result.returncode, result.stdout.strip(), result.stderr


def claims(token):
    payload = token.split('.')[1]
    return json.loads(base64.urlsafe_b64decode(payload + '=' * (-len(payload) % 4)))


def registry_token(registry):
    audience = f'{PACKAGE_HOST}/buildkite/{registry}'
    rc, token, _ = command('buildkite-agent', 'oidc', 'request-token', '--audience', audience,
                           '--lifetime', '300')
    if rc or not token:
        raise RuntimeError('OIDC issuance failed')
    payload = claims(token)
    assert payload['iss'] == 'https://agent.buildkite.com'
    assert audience in ([payload['aud']] if isinstance(payload['aud'], str) else payload['aud'])
    assert payload['organization_slug'] == 'buildkite'
    assert payload['pipeline_slug'] == 'test-engine-client-release'
    status, body, _ = get(f'{PACKAGE_API}/{registry}', 'Bearer ' + token)
    print(f'{registry}: matching legacy OIDC claims; registry show HTTP {status} (read_registries only)')
    if status == 200:
        info = json.loads(body)
        assert info['slug'] == registry
        print(f'{registry}: type={info.get("type")} ecosystem={info.get("ecosystem")} '
              f'OIDC policy present={bool(info.get("oidc_policy"))}')
    return token


def packages_inventory(registry, token, name, version):
    path = f'{PACKAGE_API}/{registry}/packages'
    url = path + '?' + urlencode({'name': name, 'per_page': 50})
    seen, matches = set(), []
    while url and len(seen) < 20:
        parts = urlsplit(url)
        query = parse_qs(parts.query)
        assert parts._replace(query='', fragment='').geturl() == path and not parts.fragment
        assert query.get('name') == [name] and url not in seen
        seen.add(url)
        status, body, _ = get(url, 'Bearer ' + token)
        if status != 201:
            print(f'{registry}: inventory page {len(seen)} HTTP {status}; state inconclusive')
            return
        data = json.loads(body)
        matches.extend({'name': row.get('name'), 'version': row.get('version'),
                        'architecture': row.get('architecture'), 'sha256': row.get('digests', {}).get('sha256')}
                       for row in data['items'] if str(row.get('version', '')).startswith(version))
        following = data['links'].get('next')
        url = urljoin(url, following) if following else None
    if url:
        raise RuntimeError('inventory page bound exceeded')
    print(f'{registry}: authenticated inventory read, {len(seen)} exact filtered pages; matching version rows: {json.dumps(matches)}')


def files():
    registry = 'bktec'
    names = ['bktec-checksums-3.6.0.txt'] + [
        f'bktec-{os_name}-{arch}-3.6.0.{ext}'
        for os_name, ext in (('darwin', 'bin'), ('linux', 'bin'), ('windows', 'exe'))
        for arch in ('amd64', 'arm64')
    ]
    base = f'{PACKAGE_HOST}/buildkite/{registry}/files'
    for name in ['bktec-checksums-3.5.0.txt'] + names:
        status, body, _ = get(f'{base}/{name}', accept='*/*', public_redirect=True)
        digest = f' sha256={hashlib.sha256(body).hexdigest()}' if status == 200 else ''
        print(f'Files {name}: public GET HTTP {status}{digest}')
    packages_inventory(registry, registry_token(registry), names[0], '3.6.0')


def error_codes(body):
    try:
        return [error['code'] for error in json.loads(body)['errors']]
    except (AttributeError, KeyError, TypeError, ValueError):
        return []


def manifests(label, base, token):
    known = False
    for tag in TAGS:
        status, body, _ = get(f'{base}/manifests/{tag}', 'Bearer ' + token, ACCEPT)
        if status == 200:
            state = 'present sha256:' + hashlib.sha256(body).hexdigest()
            if tag == TAGS[0]:
                known = True
        elif status == 404 and known and error_codes(body) == ['MANIFEST_UNKNOWN']:
            state = 'exact MANIFEST_UNKNOWN'
        else:
            state = 'inconclusive'
        print(f'{label} {tag}: HTTP {status} {state}')


def packages_oci():
    registry = 'test-engine-client-docker'
    repo = f'buildkite/{registry}/test-engine-client'
    token = registry_token(registry)
    basic = base64.b64encode(f'buildkite:{token}'.encode()).decode()
    scope = urlencode({'scope': f'repository:{repo}:pull,push'})
    status, body, _ = get(f'{PACKAGE_HOST}/v2/auth?{scope}', 'Basic ' + basic)
    print(f'Packages OCI scoped auth HTTP {status}; token exchange alone does not prove write grant')
    if status != 200:
        return
    bearer = json.loads(body)['token']
    manifests('Packages OCI', f'{PACKAGE_HOST}/v2/{repo}', bearer)
    status, body, headers = get(f'{PACKAGE_HOST}/v2/{repo}/tags/list?n=50', 'Bearer ' + bearer)
    exact_identity = False
    candidates = []
    if status == 200:
        listing = json.loads(body)
        exact_identity = listing.get('name') in (repo, f'{registry}/test-engine-client')
        candidates = sorted(set(listing.get('tags') or []) & set(TAGS))
    print(f'Packages OCI tags HTTP {status}; exact response repo={exact_identity}; '
          f'Link present={bool(headers.get("Link"))}; observed pins={candidates}; '
          'do not infer absence from an empty 404 or this diagnostic listing')


def docker_hub():
    base = '/pipelines/buildkite/test-engine-client-release/dockerhub-'
    rc, result, _ = command('aws', 'ssm', 'get-parameters', '--region', 'us-east-1',
                            '--with-decryption', '--names', base + 'user', base + 'password', '--output', 'json')
    values = {row['Name']: row['Value'] for row in json.loads(result)['Parameters']} if not rc else {}
    print(f'Docker Hub legacy SSM exact parameter read: {len(values)}/2')
    if len(values) != 2:
        return
    repo = 'buildkite/test-engine-client'
    encoded = base64.b64encode(f'{values[base + "user"]}:{values[base + "password"]}'.encode()).decode()
    request = urlencode({'service': 'registry.docker.io', 'scope': f'repository:{repo}:pull,push'})
    status, body, _ = get(f'https://auth.docker.io/token?{request}', 'Basic ' + encoded)
    if status != 200:
        print(f'Docker Hub exact-repo token HTTP {status}; grant inconclusive')
        return
    token = json.loads(body)['token']
    actions = {action for access in claims(token).get('access', [])
               if access.get('type') == 'repository' and access.get('name') == repo
               for action in access.get('actions', [])}
    print(f'Docker Hub token exact repository grant: pull={"pull" in actions} push={"push" in actions}')
    manifests('Docker Hub', f'https://registry-1.docker.io/v2/{repo}', token)


def ecr():
    env = os.environ.copy()
    for name in ('AWS_ACCESS_KEY_ID', 'AWS_SECRET_ACCESS_KEY', 'AWS_SESSION_TOKEN'):
        env[name] = env['ECR_' + name]
    account = '172840064832'
    role = f'arn:aws:iam::{account}:role/pipeline-buildkite-test-engine-client-release'
    rc, identity, _ = command('aws', 'sts', 'get-caller-identity', '--query', 'Account', '--output', 'text', env=env)
    assert not rc and identity == account
    known = False
    for tag in TAGS:
        rc, digest, error = command('aws', 'ecr-public', 'describe-images', '--region', 'us-east-1',
                                    '--registry-id', account, '--repository-name', 'test-engine-client',
                                    '--image-ids', f'imageTag={tag}', '--query', 'imageDetails[0].imageDigest',
                                    '--output', 'text', env=env)
        if not rc and digest.startswith('sha256:'):
            result = f'present {digest}'
            if tag == TAGS[0]:
                known = True
        elif known and '(ImageNotFoundException)' in error:
            result = 'exact ImageNotFoundException'
        else:
            result = 'inconclusive (describe-images failed)'
        print(f'ECR Public {tag}: {result}')
    actions = ['ecr-public:' + name for name in ('BatchCheckLayerAvailability', 'InitiateLayerUpload',
               'UploadLayerPart', 'CompleteLayerUpload', 'PutImage')]
    rc, result, _ = command('aws', 'iam', 'simulate-principal-policy', '--policy-source-arn', role,
                            '--action-names', *actions, '--resource-arns',
                            f'arn:aws:ecr-public::{account}:repository/test-engine-client',
                            '--query', 'EvaluationResults[].[EvalActionName,EvalDecision,MissingContextValues]',
                            '--output', 'json', env=env)
    print('ECR Public role policy simulation (not a live write): ' + (result if not rc else 'unavailable'))


def main():
    assert os.environ['BUILDKITE_ORGANIZATION_SLUG'] == 'buildkite'
    assert os.environ['BUILDKITE_PIPELINE_SLUG'] == 'test-engine-client-release'
    failed = False
    for label, check in [('Files', files),
                         ('DEB', lambda: packages_inventory('test-engine-client-deb', registry_token('test-engine-client-deb'), 'bktec', '3.6.0')),
                         ('RPM', lambda: packages_inventory('test-engine-client-rpm', registry_token('test-engine-client-rpm'), 'bktec', '3.6.0')),
                         ('Packages OCI', packages_oci), ('Docker Hub', docker_hub), ('ECR Public', ecr)]:
        try:
            check()
        except Exception as error:
            # HTTP exceptions can contain signed URLs; never log exception details.
            print(f'{label}: inconclusive ({type(error).__name__})')
            failed = True
    print('Preflight only: no registry write attempted or authorized by this job.')
    return int(failed)


if __name__ == '__main__':
    sys.exit(main())
